#!/usr/bin/env node
// 两步流水线，顺序固定、不可颠倒：
//   ① 生成：swag-apifox build  ->  docs/openapi.yaml（文档内容只能来自 swag）
//   ② 同步：把 openapi.yaml 原样导入 Apifox 项目，并刷新与本地有差异的数据模型
//
// 硬约束：② 阶段只「读」openapi.yaml。脚本不修改、不后处理、不重排、不补字段、
// 不格式化。① 结束后记录文件 sha256，② 上传前重新校验；不一致立即中止，
// 防止有人在生成与同步之间手改或过了一遍格式化工具。
//
// 不依赖 apifox CLI（某些版本所有命令都返回 400），直接调用其内部 API。
// 令牌只从 ~/.apifox/config.toml、环境变量或显式参数读取，绝不打印、绝不写文件。

import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';
import { spawnSync } from 'node:child_process';

const DEFAULTS = {
  // 不再内置任何项目 ID：多人共用时必须显式指定，避免把文档导错项目。
  project: process.env.APIFOX_PROJECT_ID || '',
  file: 'docs/openapi.yaml',
  config: path.join(os.homedir(), '.apifox', 'config.toml'),
  'api-base-url': process.env.APIFOX_API_BASE_URL || 'https://api.apifox.com',
  'cli-version': '2.2.9',
  'api-version': '2026-05-28',
  'max-schemas': '30',
  'max-apis': '50',
  'swag-cmd': 'swag-apifox',
  'swag-args': '--parseDependency --parseDepth 1',
};

const ALIAS = { p: 'project', f: 'file', c: 'config' };
const FLAGS = new Set([
  'verify-only', 'dry-run', 'help', 'refresh-schemas', 'no-refresh-schemas',
  'refresh-apis', 'no-refresh-apis',
  'no-generate', 'generate-only', 'quiet', 'keep-swagger',
]);
const VALUE_KEYS = new Set([
  'project', 'file', 'config', 'token', 'api-base-url', 'cli-version', 'api-version', 'timeout',
  'schemas', 'max-schemas', 'apis', 'max-apis', 'root', 'swag-cmd', 'swag-args',
]);

const USAGE = `用法:
  node pipeline.mjs [选项]           # 默认：先生成，再同步
  node pipeline.mjs --no-generate    # 只同步已有的文档
  node pipeline.mjs --generate-only  # 只生成，不联网

阶段一（生成）:
      --root <dir>          项目根目录（默认从当前目录向上找 .swaggo / go.mod）
      --swag-cmd <name>     生成命令（默认 swag-apifox）
      --swag-args <args>    透传给 swag 的参数（默认 "--parseDependency --parseDepth 1"）
      --keep-swagger        保留 docs/swagger.yaml
      --no-generate         跳过生成，直接同步已有文件
      --generate-only       只生成，不同步
      --quiet               静默 swag 输出（本脚本的统计仍会打印）

阶段二（同步）:
  -p, --project <id>        Apifox 项目 ID（必填：--project 或 APIFOX_PROJECT_ID）
  -f, --file <path>         OpenAPI 文件路径（默认 docs/openapi.yaml）
  -c, --config <path>       Apifox CLI 配置文件（默认 ~/.apifox/config.toml）
      --token <value>       显式传入访问令牌（不推荐，会出现在进程列表）
      --api-base-url <url>  API 地址（默认 https://api.apifox.com）
      --cli-version <v>     X-apifox-cli-version 头（默认 2.2.9）
      --api-version <v>     X-apifox-Api-Version 头（默认 2026-05-28）
      --timeout <ms>        单次请求超时（默认 180000）
      --refresh-schemas     导入后核对并刷新有差异的数据模型（默认开启）
      --no-refresh-schemas  只导入接口，不动数据模型
      --schemas <a,b>       只刷新指定模型（逗号分隔）
      --max-schemas <n>     单次最多刷新多少个模型（默认 30）
      --refresh-apis        导入后核对并刷新接口名称/说明/响应说明/参数定义（默认开启）
      --no-refresh-apis     不改接口元数据（import 对已存在接口一律忽略）
      --apis <list>         只刷新指定接口，格式 "get /path"（逗号分隔）
      --max-apis <n>        单次最多刷新多少个接口（默认 50）
      --verify-only         只校验令牌与项目 ID（不生成、不导入）
      --dry-run             生成照做，但不同步、不发送请求
  -h, --help                显示帮助

环境变量:
  APIFOX_ACCESS_TOKEN   访问令牌（优先于配置文件）
  APIFOX_PROJECT_ID     项目 ID（必填，等价于 --project）
  APIFOX_API_BASE_URL   默认 API 地址

退出码: 0 成功；1 失败（含生成失败、文档在生成后被改动、导入 errorCount > 0）。
`;

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const raw = argv[i];
    if (raw === '-h' || raw === '--help') { out.help = true; continue; }
    let key, val;
    if (raw.startsWith('--')) {
      key = raw.slice(2);
      const eq = key.indexOf('=');
      if (eq >= 0) { val = key.slice(eq + 1); key = key.slice(0, eq); }
    } else if (/^-[A-Za-z]$/.test(raw) && ALIAS[raw.slice(1)]) {
      key = ALIAS[raw.slice(1)];
    } else {
      throw new Error(`无法识别的参数: ${raw}`);
    }
    if (FLAGS.has(key)) { out[key] = true; continue; }
    if (!VALUE_KEYS.has(key)) throw new Error(`无法识别的参数: --${key}`);
    if (val === undefined) {
      val = argv[++i];
      if (val === undefined || val.startsWith('-')) throw new Error(`--${key} 缺少取值`);
    }
    out[key] = val;
  }
  return out;
}

function stripQuotes(s) {
  const t = String(s).trim();
  if (t.length >= 2 && ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'")))) {
    return t.slice(1, -1);
  }
  return t;
}

// 解析 CLI 的 TOML 配置：支持多账号，优先 [auth] current_account 指向的段。
function readAccessToken(cfgPath) {
  if (!fs.existsSync(cfgPath)) {
    throw new Error(`找不到 Apifox 配置 ${cfgPath}；先执行 apifox auth login --with-token <令牌>`);
  }
  const sections = {};
  let cur = null;
  for (const line of fs.readFileSync(cfgPath, 'utf8').split(/\r?\n/)) {
    const sec = line.match(/^\s*\[([^\]]+)\]\s*$/);
    if (sec) { cur = sec[1]; sections[cur] = {}; continue; }
    const kv = line.match(/^\s*([\w.-]+)\s*=\s*(.*?)\s*$/);
    if (kv && cur) sections[cur][kv[1]] = stripQuotes(kv[2]);
  }
  const current = sections.auth?.current_account;
  const candidates = [current, current && `account:${current}`];
  let section;
  for (const c of candidates) if (c && sections[c]) { section = sections[c]; break; }
  if (!section) section = Object.entries(sections).find(([k]) => k.startsWith('account:'))?.[1];
  const token = section?.access_token;
  if (!token) throw new Error(`配置 ${cfgPath} 中找不到 access_token；重新登录 apifox`);
  return token;
}

function buildHeaders(opts, token) {
  return {
    Authorization: `Bearer ${token}`,
    'X-apifox-cli-version': opts['cli-version'],
    'X-apifox-Api-Version': opts['api-version'],
    'X-Project-Id': String(opts.project),
    'Content-Type': 'application/json',
  };
}

async function request(url, { method = 'GET', body, headers, timeout }) {
  const res = await fetch(url, {
    method,
    headers,
    body,
    signal: AbortSignal.timeout(timeout),
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch { /* 非 JSON 响应 */ }
  return { status: res.status, json, text };
}

function sumCollection(coll) {
  const out = { create: 0, update: 0, ignore: 0, delete: 0, error: 0 };
  if (!coll || typeof coll !== 'object') return out;
  for (const value of Object.values(coll)) {
    if (!value || typeof value !== 'object' || !('createCount' in value)) continue;
    out.create += value.createCount || 0;
    out.update += value.updateCount || 0;
    out.ignore += value.ignoreCount || 0;
    out.delete += value.deleteCount || 0;
    out.error += value.errorCount || 0;
  }
  return out;
}

function fmt(c) {
  return `新增 ${c.create} / 更新 ${c.update} / 忽略 ${c.ignore} / 删除 ${c.delete} / 错误 ${c.error}`;
}

// ---------------------------------------------------------------------------
// 阶段一：生成（文档内容只能来自 swag，脚本不参与改写）
// ---------------------------------------------------------------------------

function findProjectRoot(start) {
  let dir = path.resolve(start);
  for (let i = 0; i < 10; i++) {
    if (fs.existsSync(path.join(dir, '.swaggo'))) return dir;
    if (fs.existsSync(path.join(dir, 'go.mod')) && fs.existsSync(path.join(dir, 'main.go'))) return dir;
    const up = path.dirname(dir);
    if (up === dir) break;
    dir = up;
  }
  return '';
}

const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

function countDocStats(yaml) {
  const lines = yaml.split(/\r?\n/);
  let paths = 0;
  let methods = 0;
  let inPaths = false;
  for (const line of lines) {
    if (/^paths:\s*$/.test(line)) { inPaths = true; continue; }
    if (inPaths && /^\S/.test(line)) { inPaths = false; continue; }
    if (!inPaths) continue;
    if (/^ {2}\/\S*:\s*$/.test(line)) paths++;
    else if (/^ {4}(get|post|put|patch|delete|head|options):\s*$/.test(line)) methods++;
  }
  return { paths, methods };
}

function runGenerate(opts) {
  const cwd = process.cwd();
  const root = opts.root ? path.resolve(opts.root) : findProjectRoot(cwd);
  if (!root) {
    throw new Error('当前目录看起来不是 Go + swag 项目（找不到 .swaggo 或 go.mod+main.go）；'
      + '用 --root 指定项目根，或加 --no-generate 只同步已有文件');
  }
  const cmd = opts['swag-cmd'];
  const passthrough = String(opts['swag-args'] || '').split(/\s+/).filter(Boolean);
  const args = ['build', '--color', 'never'];
  if (opts.quiet) args.push('--quiet');
  if (opts['keep-swagger']) args.push('--keep-swagger');
  args.push('--', ...passthrough);

  console.log(`[1/2] 生成：${cmd} ${args.join(' ')}`);
  console.log(`[1/2] 工作目录 ${root}`);
  const win = process.platform === 'win32';
  const r = spawnSync(cmd, args, { cwd: root, stdio: 'inherit', shell: win });

  if (r.error) {
    if (r.error.code === 'ENOENT') {
      throw new Error(`找不到生成命令 ${cmd}；确认已安装并加入 PATH，或用 --swag-cmd 指定绝对路径`);
    }
    throw new Error(`执行 ${cmd} 失败：${r.error.message}`);
  }
  if (r.status !== 0) {
    throw new Error(`${cmd} 退出码 ${r.status ?? 'null'}，生成失败；先修好 swag 报错再同步`);
  }

  const file = path.resolve(cwd, opts.file);
  if (!fs.existsSync(file)) throw new Error(`生成命令没有产出 ${file}（检查 -o / --root 是否一致）`);
  const buf = fs.readFileSync(file);
  const stats = countDocStats(buf.toString('utf8'));
  const hash = sha256(buf);
  console.log(`[1/2] 产出 ${file}（${buf.length} 字节，${stats.paths} 个路径 / ${stats.methods} 个操作，`
    + `sha256 ${hash.slice(0, 12)}…）`);
  return { file, hash, size: buf.length, stats };
}

// ---------------------------------------------------------------------------
// 本地 OpenAPI 模型解析：只针对 swag 生成的 components.schemas 区域，按缩进取值。
// 目的不是做通用 YAML 解析，而是拿到「属性名/顺序/必填/说明/类型/示例/枚举」这些
// 足以判断差异并做最小改动的信息；遇到无法安全重建的结构就跳过该模型。
// ---------------------------------------------------------------------------

function yamlScalar(v) {
  const s = String(v).trim();
  if (s === '') return '';
  if (s.startsWith('"') && s.endsWith('"') && s.length >= 2) {
    // 双引号内的转义（swag 常见 \\n \\t \\" ）还原
    return s.slice(1, -1).replace(/\\n/g, '\n').replace(/\\t/g, '\t').replace(/\\"/g, '"').replace(/\\\\/g, '\\');
  }
  if (s.startsWith("'") && s.endsWith("'") && s.length >= 2) return s.slice(1, -1).replace(/''/g, "'");
  return s;
}

function yamlTypedScalar(v) {
  const s = String(v).trim();
  if (s === '') return '';
  if (/^-?\d+$/.test(s)) return Number(s);
  if (/^-?\d*\.\d+$/.test(s)) return Number(s);
  if (s === 'true') return true;
  if (s === 'false') return false;
  return yamlScalar(s);
}

const indentOf = (line) => line.length - line.trimStart().length;

function readChildBlock(lines, startIndex, parentIndent) {
  let i = startIndex;
  const out = [];
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }
    if (indentOf(line) <= parentIndent) break;
    out.push(line);
    i++;
  }
  return { childLines: out, next: i };
}

// 解析单个属性块（属性名行之后、缩进 >= 8 的行）
function parsePropBlock(lines) {
  const spec = {
    description: '',
    type: '',
    example: undefined,
    form: '',
    ref: '',
    itemsRef: '',
    itemsType: '',
    enumValues: [],
    apifoxEnum: null,
    unsupported: [],
  };
  const KEYS_OK = new Set([
    'description', 'type', 'example', 'form', '$ref', 'items', 'enum', 'x-apifox-enum',
    'uniqueItems', 'format', 'x-nullable', 'extensions', 'nullable', 'x-enum-varnames', 'x-enum-comments',
  ]);

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const ind = indentOf(line);
    const t = line.trim();

    if (ind === 10) {
      const m = t.match(/^([\w$.-]+):\s*(.*)$/);
      if (m) {
        const key = m[1];
        const val = m[2];
        if (!KEYS_OK.has(key)) spec.unsupported.push(key);
        if (key === 'description') {
          spec.description = yamlScalar(val);
          // description 可能是折行的纯量，后续更深缩进且不像键的行要拼接
          let j = i + 1;
          while (j < lines.length && indentOf(lines[j]) > 10) {
            const nxt = lines[j].trim();
            if (/^[\w$.-]+:\s/.test(nxt) || nxt.startsWith('- ')) break;
            spec.description += ' ' + nxt;
            j++;
          }
          i = j;
          continue;
        }
        if (key === 'type') spec.type = yamlScalar(val);
        else if (key === 'example') {
          if (val) {
            spec.example = yamlTypedScalar(val);
            i++;
            continue;
          }
          // 数组示例写成 `example:` 加同缩进/更深缩进的 `- value`
          const items = [];
          let j = i + 1;
          while (j < lines.length) {
            const cl = lines[j];
            if (!cl.trim()) { j++; continue; }
            const cind = indentOf(cl);
            if (cind < 10) break;
            if (cind === 10 && !cl.trim().startsWith('- ')) break;
            const mm = cl.trim().match(/^-\s*(.*)$/);
            if (!mm) break;
            items.push(yamlTypedScalar(mm[1]));
            j++;
          }
          spec.example = items.length ? items : undefined;
          i = j;
          continue;
        }
        else if (key === 'form') spec.form = yamlScalar(val);
        else if (key === '$ref') spec.ref = stripQuotes(val);
        else if (key === 'items') {
          const { childLines, next } = readChildBlock(lines, i + 1, 10);
          for (const cl of childLines) {
            const cm = cl.trim().match(/^\$ref:\s*(.*)$/);
            if (cm) spec.itemsRef = stripQuotes(cm[1]);
            const tm = cl.trim().match(/^type:\s*(.*)$/);
            if (tm) spec.itemsType = yamlScalar(tm[1]);
          }
          i = next;
          continue;
        } else if (key === 'enum') {
          // swag 生成的写法是 `enum:` 与 `- 1` 同缩进，也兼容子层缩进
          if (val) {
            const inline = stripQuotes(val).replace(/^\[|\]$/g, '');
            for (const part of inline.split(',')) if (part.trim()) spec.enumValues.push(yamlTypedScalar(part));
            i++;
            continue;
          }
          const items = [];
          let j = i + 1;
          while (j < lines.length) {
            const cl = lines[j];
            if (!cl.trim()) { j++; continue; }
            const cind = indentOf(cl);
            if (cind === 10 && cl.trim().startsWith('- ')) {
              items.push(yamlTypedScalar(cl.trim().slice(2)));
              j++;
              continue;
            }
            if (cind > 10) { j++; continue; }
            break;
          }
          spec.enumValues = items;
          i = j;
          continue;
        } else if (key === 'x-apifox-enum') {
          const { childLines, next } = readChildBlock(lines, i + 1, 10);
          const items = [];
          let curItem = null;
          for (const cl of childLines) {
            const ct = cl.trim();
            const vm = ct.match(/^-\s*value:\s*(.*)$/);
            if (vm) { curItem = { value: yamlTypedScalar(vm[1]), name: '', description: '' }; items.push(curItem); continue; }
            if (!curItem) continue;
            const nm = ct.match(/^name:\s*(.*)$/);
            if (nm) { curItem.name = yamlScalar(nm[1]); continue; }
            const dm = ct.match(/^description:\s*(.*)$/);
            if (dm) curItem.description = yamlScalar(dm[1]);
          }
          if (items.length) spec.apifoxEnum = items;
          i = next;
          continue;
        }
        i++;
        continue;
      }
    }
    i++;
  }
  return spec;
}

function parseSchemaBlock(lines) {
  const info = { description: '', type: '', required: [], propOrder: [], props: new Map() };
  let section = '';
  let prop = null;
  let propLines = [];

  const flushProp = () => {
    if (prop) {
      info.props.set(prop, parsePropBlock(propLines));
      info.propOrder.push(prop);
    }
    prop = null;
    propLines = [];
  };

  for (const raw of lines) {
    const line = raw.replace(/\s+$/, '');
    if (!line.trim()) continue;
    const ind = indentOf(line);
    const t = line.trim();

    // schema 直系层级（缩进 6）
    if (ind === 6) {
      flushProp();
      if (t.startsWith('- ')) {
        if (section === 'required') info.required.push(yamlScalar(t.slice(2)));
        continue;
      }
      const m = t.match(/^([\w$.-]+):\s*(.*)$/);
      if (!m) { section = ''; continue; }
      const key = m[1];
      const val = m[2];
      if (key === 'description') {
        info.description = yamlScalar(val);
        section = 'description';
      } else if (key === 'type') {
        info.type = yamlScalar(val);
        section = '';
      } else if (key === 'required') {
        section = 'required';
        info.required = [];
      } else if (key === 'properties') {
        section = 'properties';
      } else {
        section = '';
      }
      continue;
    }

    if (section === 'description' && ind > 6) { info.description += ' ' + t; continue; }
    if (section !== 'properties') continue;

    if (ind === 8) {
      flushProp();
      const m = t.match(/^([\w$.-]+):\s*$/);
      if (m) { prop = m[1]; propLines = []; }
      continue;
    }
    if (prop) propLines.push(line);
  }
  flushProp();
  return info;
}

function parseLocalSchemas(yaml) {
  const lines = yaml.split(/\r?\n/);
  let start = -1;
  for (let i = 0; i < lines.length; i++) {
    if (/^  schemas:\s*$/.test(lines[i])) { start = i + 1; break; }
  }
  const out = new Map();
  if (start < 0) return out;

  let name = null;
  let block = [];
  const flush = () => { if (name) out.set(name, parseSchemaBlock(block)); };

  for (let i = start; i < lines.length; i++) {
    const line = lines[i];
    if (line && !/^\s/.test(line)) break;          // 顶层键，区域结束
    if (/^  \S/.test(line)) break;                 // components 下其它子键
    const m = line.match(/^    (\S[^:]*):\s*$/);
    if (m) {
      flush();
      name = m[1].trim();
      block = [];
      continue;
    }
    if (name) block.push(line);
  }
  flush();
  return out;
}

// ---------------------------------------------------------------------------
// 本地 OpenAPI 接口解析：解析 summary / description / 各响应说明 / 入参定义。
// 只用来比对元数据（名称、说明、响应说明、缺失的参数）；请求体与响应 schema
// 由数据模型刷新负责，这里不做 schema 级比对。
// ---------------------------------------------------------------------------

// 读取 key: 后面的标量值，兼容 swag 的「plain 折叠续行」与 | / > 块标量。
function readScalarAt(lines, i, keyIndent) {
  const line = lines[i];
  const m = line.match(/^(\s*)([^\s:#][^:]*):(?:\s+(.*))?$/);
  if (!m) return { value: '', next: i + 1 };
  let raw = (m[3] || '').trim();
  let j = i + 1;
  if (/^[|>][+-]?\d*$/.test(raw)) {
    const parts = [];
    while (j < lines.length) {
      const l = lines[j];
      if (l.trim() === '') { parts.push(''); j++; continue; }
      if (indentOf(l) <= keyIndent) break;
      parts.push(l.trim());
      j++;
    }
    return { value: parts.join('\n').trim(), next: j };
  }
  if (raw === '') return { value: '', next: j };
  const parts = [raw];
  while (j < lines.length) {
    const l = lines[j];
    if (!l.trim()) break;
    if (indentOf(l) <= keyIndent) break;
    // 子键（缩进 +2 且形如 key:）说明本字段已结束
    if (indentOf(l) === keyIndent + 2 && /^[^\s:#][^:]*:(\s|$)/.test(l.trim())) break;
    parts.push(l.trim());
    j++;
  }
  return { value: parts.join(' ').replace(/\s+/g, ' ').trim(), next: j };
}

// 解析 method 块下的 parameters 列表（swag 输出：indent 6 的 "- name: ..."，字段 indent 8，schema indent 10）。
function parseLocalParameters(lines, start) {
  const params = [];
  let cur = null;
  let j = start + 1;
  while (j < lines.length) {
    const l = lines[j];
    if (!l.trim()) { j++; continue; }
    const ind = indentOf(l);
    const t = l.trim();
    if (ind === 6) {
      if (/^-\s+/.test(t)) {
        if (cur) params.push(cur);
        cur = { description: '', in: 'query', name: '', required: false, type: '', enum: [] };
        const mm = t.replace(/^-\s+/, '').match(/^([^:]+):\s*(.*)$/);
        if (mm) cur[mm[1]] = yamlScalar(mm[2]);
        j++;
        continue;
      }
      break; // 下一个 method 级字段
    }
    if (ind < 6) break;
    if (!cur) { j++; continue; }
    if (ind === 8) {
      const mm = t.match(/^([^:]+):\s*(.*)$/);
      if (!mm) { j++; continue; }
      const k = mm[1];
      if (k === 'schema') {
        let s = j + 1;
        let inEnum = false;
        let inExample = false;
        while (s < lines.length) {
          const sl = lines[s];
          if (!sl.trim()) { s++; continue; }
          if (indentOf(sl) <= 8) break;
          const st = sl.trim();
          if (indentOf(sl) === 10) {
            if (/^enum:\s*$/.test(st)) { inEnum = true; inExample = false; s++; continue; }
            // 数组示例写成块状列表（swag 对 array 的 example 就是这样展开的），
            // 标量示例写在同一行；两种都必须读进来，否则数组参数永远比对不上。
            if (/^example:\s*$/.test(st)) { inExample = true; cur.example = []; s++; continue; }
            const em0 = st.match(/^example:\s*(.*)$/);
            if (em0) { cur.example = yamlTypedScalar(em0[1]); inExample = false; s++; continue; }
            if (/^[^\s:#][^:]*:/.test(st)) { inEnum = false; inExample = false; }
            const em = st.match(/^type:\s*(.*)$/);
            if (em) cur.type = yamlScalar(em[1]);
            if (inEnum && /^-\s*/.test(st)) cur.enum.push(yamlTypedScalar(st.replace(/^-\s*/, '')));
            if (inExample && /^-\s*/.test(st)) cur.example.push(yamlTypedScalar(st.replace(/^-\s*/, '')));
          }
          s++;
        }
        j = s;
        continue;
      }
      if (k === 'required') cur.required = mm[2].trim() === 'true';
      // example 要保类型：swag 把 integer/boolean 的示例输出成裸标量，按字符串存会与 schema 类型不符
      else if (k === 'example') cur.example = yamlTypedScalar(mm[2]);
      else cur[k] = yamlScalar(mm[2]);
    }
    j++;
  }
  if (cur) params.push(cur);
  return { params, next: j };
}

// 解析 multipart/form-data 的示例：swag 把 formData 参数的 example 写在
// requestBody.content.multipart/form-data.schema.properties.<name>.example，
// 不在参数列表里，所以单独抓一份（name -> 示例值）。
function parseFormDataExamples(lines, start) {
  const out = new Map();
  let inProps = false;
  let propIndent = -1;
  let curName = '';
  for (let j = start + 1; j < lines.length; j++) {
    const l = lines[j];
    if (!l.trim()) continue;
    const ind = indentOf(l);
    if (ind <= 6) break;
    const t = l.trim();
    if (/^properties:\s*$/.test(t)) { inProps = true; propIndent = ind; continue; }
    if (!inProps) continue;
    if (ind === propIndent + 2) {
      const m = t.match(/^([^\s:#][^:]*):\s*$/);
      curName = m ? m[1] : '';
      continue;
    }
    if (ind > propIndent + 2 && curName) {
      const m = t.match(/^example:\s*(.*)$/);
      if (m) out.set(curName, yamlTypedScalar(m[1]));
    }
  }
  return out;
}

function parseLocalOperations(yaml) {
  const lines = yaml.split(/\r?\n/);
  const ops = new Map();
  let inPaths = false;
  let curPath = '';
  let curOp = null;

  const flush = () => {
    if (curPath && curOp) ops.set(`${curOp.method} ${curPath}`, curOp);
    curOp = null;
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^paths:\s*$/.test(line)) { inPaths = true; continue; }
    if (inPaths && /^\S/.test(line)) { inPaths = false; flush(); curPath = ''; continue; }
    if (!inPaths) continue;

    const pm = line.match(/^ {2}(\/\S*):\s*$/);
    if (pm) { flush(); curPath = pm[1]; continue; }
    const mm = line.match(/^ {4}(get|post|put|patch|delete|head|options):\s*$/);
    if (mm && curPath) {
      flush();
      curOp = { method: mm[1], path: curPath, name: '', description: '', responses: {}, parameters: [], formExamples: new Map() };
      continue;
    }
    if (!curOp) continue;

    const indent = indentOf(line);
    const key = line.trim().match(/^([A-Za-z_][\w.$@-]*):/);
    if (!key) continue;

    if (indent === 6 && (key[1] === 'summary' || key[1] === 'description')) {
      const s = readScalarAt(lines, i, indent);
      if (key[1] === 'summary') curOp.name = s.value;
      else curOp.description = s.value;
      i = s.next - 1;
      continue;
    }
    if (indent === 6 && key[1] === 'parameters') {
      const p = parseLocalParameters(lines, i);
      curOp.parameters = p.params;
      i = p.next - 1;
      continue;
    }
    if (indent === 6 && key[1] === 'requestBody') {
      curOp.formExamples = parseFormDataExamples(lines, i);
      continue;
    }
    if (indent === 6 && key[1] === 'responses') {
      // responses 下每个状态码缩进 8，其 description 缩进 10
      let j = i + 1;
      let code = '';
      while (j < lines.length) {
        const l = lines[j];
        if (!l.trim()) { j++; continue; }
        const ind = indentOf(l);
        if (ind <= 6) break;
        if (ind === 8) {
          const cm = l.trim().match(/^"?(\d{3}|default)"?:\s*$/);
          code = cm ? cm[1] : '';
          j++;
          continue;
        }
        if (ind === 10 && code && /^description:/.test(l.trim())) {
          const s = readScalarAt(lines, j, ind);
          curOp.responses[code] = s.value;
          code = '';
          j = s.next;
          continue;
        }
        j++;
      }
      i = j - 1;
    }
  }
  flush();
  return ops;
}

// ---------------------------------------------------------------------------
// 与 Apifox 端模型对比并生成最小改动
// ---------------------------------------------------------------------------

const refName = (ref) => {
  const m = String(ref).match(/#\/(?:components\/schemas|definitions)\/(.+)$/);
  return m ? m[1] : '';
};

function quickDiffers(local, remoteSchema) {
  const props = remoteSchema?.properties || {};
  const remoteNames = Object.keys(props);
  if (remoteNames.length !== local.propOrder.length) return true;
  for (let i = 0; i < local.propOrder.length; i++) {
    if (local.propOrder[i] !== remoteNames[i]) return true;
  }
  const remoteReq = new Set(remoteSchema?.required || []);
  if (remoteReq.size !== local.required.length) return true;
  for (const r of local.required) if (!remoteReq.has(r)) return true;
  if ((remoteSchema?.description || '') !== (local.description || '')) return true;
  for (const name of local.propOrder) {
    const spec = local.props.get(name);
    if (!spec.ref && spec.type && props[name]?.type !== spec.type) return true;
    if (spec.example === undefined) continue;
    const want = Array.isArray(spec.example) ? spec.example : [spec.example];
    if (JSON.stringify(props[name]?.examples) !== JSON.stringify(want)) return true;
    if (spec.enumValues.length && JSON.stringify(props[name]?.enum) !== JSON.stringify(spec.enumValues)) return true;
    if (spec.apifoxEnum && JSON.stringify(props[name]?.['x-apifox-enum']) !== JSON.stringify(spec.apifoxEnum)) return true;
  }
  return false;
}

function buildPropValue(spec, nameToId) {
  if (spec.unsupported.length) {
    return { skip: `属性含暂不支持自动重建的字段 ${spec.unsupported.join('/')}` };
  }
  if (spec.ref) {
    const name = refName(spec.ref);
    const id = name && nameToId.get(name);
    if (!id) return { skip: `引用 ${spec.ref} 在 Apifox 中找不到对应模型` };
    const v = { $ref: `#/definitions/${id}` };
    if (spec.description) v.description = spec.description;
    if (spec.form) v.form = spec.form;
    if (spec.enumValues.length) v.enum = spec.enumValues;
    if (spec.apifoxEnum) v['x-apifox-enum'] = spec.apifoxEnum;
    if (spec.example !== undefined) v.examples = [spec.example];
    return { value: v };
  }

  const v = {};
  if (spec.description) v.description = spec.description;
  if (spec.type === 'array') {
    v.type = 'array';
    if (spec.itemsRef) {
      const name = refName(spec.itemsRef);
      const id = name && nameToId.get(name);
      if (!id) return { skip: `数组元素引用 ${spec.itemsRef} 在 Apifox 中找不到对应模型` };
      v.items = { $ref: `#/definitions/${id}` };
    } else if (spec.itemsType) {
      v.items = { type: spec.itemsType };
    }
    v.uniqueItems = false;
  } else if (spec.type) {
    v.type = spec.type;
  }
  if (spec.enumValues.length) v.enum = spec.enumValues;
  if (spec.apifoxEnum) v['x-apifox-enum'] = spec.apifoxEnum;
  if (spec.example !== undefined) v.examples = [spec.example];
  if (spec.form) v.form = spec.form;
  return { value: v };
}

function planUpdate(local, remote, nameToId) {
  const remoteSchema = remote?.jsonSchema || {};
  if (!local.propOrder.length) return { skip: '本地无 properties（标量别名模型），跳过' };
  if (remoteSchema.type && remoteSchema.type !== 'object') {
    return { skip: `Apifox 端类型为 ${remoteSchema.type}，不是对象` };
  }
  const remoteProps = remoteSchema.properties || {};
  const next = structuredClone(remoteSchema);
  const nextProps = {};
  const added = [];
  const removed = [];
  const patched = [];

  for (const key of Object.keys(remoteProps)) {
    if (!local.props.has(key)) removed.push(key);
  }

  for (const name of local.propOrder) {
    const spec = local.props.get(name);
    const exist = remoteProps[name];
    if (!exist) {
      const built = buildPropValue(spec, nameToId);
      if (built.skip) return { skip: `新增属性 ${name}：${built.skip}` };
      nextProps[name] = built.value;
      added.push(name);
      continue;
    }
    const clone = structuredClone(exist);
    if (!spec.ref && spec.type && clone.type !== spec.type) {
      const before = clone.type;
      clone.type = spec.type;
      patched.push(`${name} 类型 ${before}→${spec.type}`);
    }
    if (spec.description && String(clone.description || '') !== spec.description) {
      clone.description = spec.description;
      patched.push(`${name} 说明`);
    }
    if (spec.example !== undefined) {
      const want = Array.isArray(spec.example) ? spec.example : [spec.example];
      if (JSON.stringify(clone.examples) !== JSON.stringify(want)) {
        clone.examples = want;
        patched.push(`${name} 示例`);
      }
    }
    if (spec.enumValues.length && JSON.stringify(clone.enum) !== JSON.stringify(spec.enumValues)) {
      clone.enum = spec.enumValues;
      patched.push(`${name} 枚举`);
    }
    if (spec.apifoxEnum && JSON.stringify(clone['x-apifox-enum']) !== JSON.stringify(spec.apifoxEnum)) {
      clone['x-apifox-enum'] = spec.apifoxEnum;
      patched.push(`${name} 枚举说明`);
    }
    nextProps[name] = clone;
  }

  const remoteReq = JSON.stringify(remoteSchema.required || []);
  const nextRequired = local.required.filter((r) => local.props.has(r));
  const reqChanged = JSON.stringify(nextRequired) !== remoteReq;

  const remoteOrders = JSON.stringify(remoteSchema['x-apifox-orders'] || []);
  const ordersChanged = remoteOrders !== JSON.stringify(local.propOrder);

  const descChanged = (remoteSchema.description || '') !== local.description;
  if (descChanged) next.description = local.description;

  const changed = added.length || removed.length || patched.length || reqChanged || ordersChanged
    || descChanged || Object.keys(nextProps).length !== Object.keys(remoteProps).length;
  if (!changed) return { changed: false };

  next.type = 'object';
  next.properties = nextProps;
  next.required = nextRequired;
  next['x-apifox-orders'] = local.propOrder;
  if (local.description) next.description = local.description;

  return { changed: true, jsonSchema: next, added, removed, patched, reqChanged, descChanged };
}

function anyValue(v, def) {
  return v === undefined || v === null ? def : v;
}

// ---------------------------------------------------------------------------
// 自动刷新数据模型
// ---------------------------------------------------------------------------

async function fetchRemoteSchemas({ base, project, headers, timeout }) {
  const map = new Map();
  const perPage = 500;
  let page = 1;
  let total = Infinity;
  while (map.size < total && page <= 10) {
    const res = await request(`${base}/api/v1/api-schemas?page=${page}&perPage=${perPage}`, { headers, timeout });
    if (res.status !== 200 || !res.json?.success) {
      throw new Error(`拉取数据模型列表失败 HTTP ${res.status}：${res.json?.errorMessage || res.text.slice(0, 200)}`);
    }
    total = Number(res.json.meta?.total ?? (res.json.data?.length || 0));
    for (const item of res.json.data || []) {
      if (item?.name) map.set(item.name, item);
    }
    if (!(res.json.data || []).length) break;
    page++;
  }
  return map;
}

async function refreshSchemas({ base, project, headers, token, timeout, local, opts, only }) {
  const remote = await fetchRemoteSchemas({ base, project, headers, timeout });
  const nameToId = new Map([...remote.entries()].map(([name, item]) => [name, item.id]));

  let names = [...local.keys()].filter((n) => remote.has(n));
  if (only.length) {
    const missing = only.filter((n) => !local.has(n) || !remote.has(n));
    if (missing.length) console.log(`[schemas] 指定模型未同时存在于本地与 Apifox：${missing.join(', ')}`);
    names = only.filter((n) => local.has(n) && remote.has(n));
    console.log(`[schemas] 已限定只核对：${names.join(', ') || '(无)'}`);
  }

  const candidates = names.filter((n) => quickDiffers(local.get(n), remote.get(n).jsonSchema));
  console.log(`[schemas] 本地模型 ${local.size} 个，Apifox 模型 ${remote.size} 个；待核对 ${candidates.length} 个`);
  if (!candidates.length) return { updated: 0, skipped: 0, unchanged: names.length };

  const max = Number(opts['max-schemas'] || 30);
  let updated = 0;
  let skipped = 0;
  const skips = [];

  for (const name of candidates) {
    if (updated >= max) {
      console.warn(`[schemas] 已达单次刷新上限 ${max}，剩余 ${candidates.length - updated} 个候选未处理（可用 --max-schemas 调大）`);
      break;
    }
    const listed = remote.get(name);
    let detail;
    try {
      detail = await request(`${base}/api/v1/api-schemas/${listed.id}`, { headers, timeout });
    } catch (err) {
      skipped++; skips.push(`${name}：读取失败 ${err.message}`); continue;
    }
    if (detail.status !== 200 || !detail.json?.success) {
      skipped++; skips.push(`${name}：GET HTTP ${detail.status}`); continue;
    }
    const current = detail.json.data || {};
    const plan = planUpdate(local.get(name), current, nameToId);
    if (plan.skip) { skipped++; skips.push(`${name}：${plan.skip}`); continue; }
    if (!plan.changed) continue;

    const body = JSON.stringify({
      name: current.name || name,
      displayName: anyValue(current.displayName, ''),
      jsonSchema: plan.jsonSchema,
      description: anyValue(plan.jsonSchema.description, anyValue(current.description, '')),
      folderId: anyValue(current.folderId, 0),
      moduleId: current.moduleId,
      visibility: anyValue(current.visibility, 'INHERITED'),
    });
    const put = await request(`${base}/api/v1/api-schemas/${listed.id}`, {
      method: 'PUT', body, headers, timeout,
    });
    if (put.status < 200 || put.status >= 300 || !put.json?.success) {
      skipped++;
      skips.push(`${name}：PUT HTTP ${put.status} ${put.json?.errorMessage || put.text.slice(0, 120)}`);
      continue;
    }
    updated++;
    const parts = [];
    if (plan.added.length) parts.push(`新增 ${plan.added.join(', ')}`);
    if (plan.removed.length) parts.push(`删除 ${plan.removed.join(', ')}`);
    if (plan.patched.length) parts.push(`更新 ${plan.patched.join(', ')}`);
    if (plan.reqChanged) parts.push(`必填 ${plan.jsonSchema.required.join(', ') || '(空)'}`);
    if (plan.descChanged) parts.push('模型说明');
    console.log(`[schemas] 已更新 ${name}：${parts.join('；') || '结构顺序调整'}`);
  }

  for (const s of skips) console.warn(`[schemas] 跳过 ${s}`);
  console.log(`[schemas] 完成：更新 ${updated} / 无需变更 ${candidates.length - updated - skipped} / 跳过 ${skipped}`);
  return { updated, skipped, unchanged: names.length - candidates.length };
}

// ---------------------------------------------------------------------------
// 自动刷新接口元数据（名称 / 说明 / 响应说明）
// import-data 对「已存在」的接口一律忽略（update=0），所以注解里改过的描述
// 不会自己到达 Apifox，这里逐条比对后做最小 PUT。参数与 schema 不动，
// 那部分由数据模型刷新负责。
// ---------------------------------------------------------------------------

async function fetchRemoteApis({ base, project, headers, timeout }) {
  const map = new Map();
  const perPage = 500;
  let page = 1;
  let total = Infinity;
  let seen = 0;
  while (seen < total && page <= 20) {
    const res = await request(`${base}/api/v1/projects/${project}/http-apis?page=${page}&perPage=${perPage}`, { headers, timeout });
    if (res.status !== 200 || !res.json?.success) {
      throw new Error(`拉取接口列表失败 HTTP ${res.status}：${res.json?.errorMessage || res.text.slice(0, 200)}`);
    }
    total = Number(res.json.meta?.total ?? (res.json.data?.length || 0));
    const rows = res.json.data || [];
    for (const item of rows) {
      if (!item?.method || !item?.path) continue;
      map.set(`${String(item.method).toLowerCase()} ${item.path}`, item);
    }
    seen += rows.length;
    if (!rows.length) break;
    page++;
  }
  return map;
}

// 把本地解析出的参数转成「期望写入」的形态（不含 id/enable）；枚举说明从 description 的
// 「0-全部，1-已关联」还原成 Apifox 的 x-apifox-enum。新增参数与已有参数的类型刷新共用这一份。
function desiredParam(p) {
  const type = p.type || 'string';
  const schema = { type };
  if (p.enum.length) {
    schema.enum = p.enum;
    const labels = [];
    const re = /(\d+)\s*[-:]\s*([^，,；;]+)/g;
    let m;
    while ((m = re.exec(p.description || '')) !== null) {
      labels.push({ value: Number(m[1]), name: '', description: m[2].trim() });
    }
    if (labels.length === p.enum.length) schema['x-apifox-enum'] = labels;
  }
  // 示例值放在 schema.example（Apifox 参数对象没有顶层 example 字段）
  if (p.example !== undefined && p.example !== null && p.example !== '') schema.example = p.example;
  return { type, description: p.description || '', schema };
}

function buildApifoxParam(p) {
  const want = desiredParam(p);
  return {
    id: 'p' + crypto.randomBytes(4).toString('hex'),
    name: p.name,
    required: !!p.required,
    enable: true,
    description: want.description,
    type: want.type,
    schema: want.schema,
  };
}

// 合并已存在参数的 schema：只覆盖本脚本负责的 type / enum / x-apifox-enum / example，
// 其余键（title、mock 等线上配置）原样保留。example 只在本地写了示例时才覆盖。
function mergeParamSchema(existing, want) {
  const next = { ...(existing || {}) };
  next.type = want.type;
  if (want.enum) next.enum = want.enum;
  else delete next.enum;
  if (want['x-apifox-enum']) next['x-apifox-enum'] = want['x-apifox-enum'];
  else delete next['x-apifox-enum'];
  if (want.example !== undefined) next.example = want.example;
  return next;
}

// 已存在参数与本地的差异点；空数组表示一致。说明只在本地写了非空文案时才比对，
// 避免把线上手工补的说明冲成空。
function paramDelta(existing, want) {
  const notes = [];
  if ((existing.type || '') !== (want.type || '')) {
    notes.push(`类型 ${existing.type || '(空)'}→${want.type}`);
  }
  if (want.description && (existing.description || '') !== want.description) notes.push('说明');
  const cur = existing.schema || {};
  if (JSON.stringify(cur.enum || []) !== JSON.stringify(want.schema.enum || [])) notes.push('枚举');
  if (JSON.stringify(cur['x-apifox-enum'] || []) !== JSON.stringify(want.schema['x-apifox-enum'] || [])) {
    notes.push('枚举说明');
  }
  // 示例只在本地写了才比对，本地没写就保留线上手工加的示例
  if (want.schema.example !== undefined && JSON.stringify(cur.example) !== JSON.stringify(want.schema.example)) {
    notes.push(`示例 ${JSON.stringify(cur.example ?? null)}→${JSON.stringify(want.schema.example)}`);
  }
  return notes;
}

// 只「补」不「清」：本地没有写的字段（未加 @Description / @Summary）保留线上内容，
// 避免把在 Apifox 上手工补过的说明冲成空。
function apiMetaDiff(op, remote) {
  const diffs = [];
  const addedParams = [];
  if (op.name && op.name !== (remote.name || '')) diffs.push(`名称「${remote.name || '(空)'}」→「${op.name}」`);
  if (op.description && op.description !== (remote.description || '')) diffs.push('接口说明');
  const remoteResp = new Map((remote.responses || []).map((r) => [String(r.code), r.description || '']));
  const respChanges = [];
  for (const [code, desc] of Object.entries(op.responses || {})) {
    if (!desc) continue;
    if (!remoteResp.has(code)) continue;
    if (remoteResp.get(code) !== desc) {
      respChanges.push({ code, desc });
      diffs.push(`响应 ${code} 说明`);
    }
  }
  // 参数按 in + name 比对：本地没有的线上参数原样保留（只补不清），
  // 两边都有但类型/说明/枚举不一致的记为待刷新。
  const remoteParams = remote.parameters || {};
  const paramChanges = [];
  for (const p of op.parameters || []) {
    if (!p.name || !p.in) continue;
    const list = remoteParams[p.in] || [];
    const hit = list.find((x) => x.name === p.name);
    if (!hit) {
      addedParams.push(p);
      continue;
    }
    const want = desiredParam(p);
    const notes = paramDelta(hit, want);
    if (notes.length) {
      paramChanges.push({ in: p.in, name: p.name, want, notes });
      diffs.push(`参数 ${p.in}:${p.name}（${notes.join('、')}）`);
    }
  }
  if (addedParams.length) diffs.push(`新增参数 ${addedParams.map((p) => `${p.in}:${p.name}`).join(', ')}`);

  // multipart/form-data 的示例：Apifox 把它放在 requestBody.parameters[i].example，
  // 与 query 参数不是一套结构，单独比对。
  const formChanges = [];
  const formExamples = op.formExamples || new Map();
  const remoteForms = new Map(
    ((remote.requestBody || {}).parameters || []).map((x) => [x.name, x.example]),
  );
  for (const [name, val] of formExamples) {
    if (!remoteForms.has(name)) continue;
    if (String(remoteForms.get(name) ?? '') === String(val)) continue;
    formChanges.push({ name, value: val });
    diffs.push(`formData:${name} 示例`);
  }

  return { diffs, respChanges, addedParams, paramChanges, formChanges };
}

async function refreshApis({ base, project, headers, timeout, ops, opts, only }) {
  const remote = await fetchRemoteApis({ base, project, headers, timeout });

  let keys = [...ops.keys()].filter((k) => remote.has(k));
  if (only.length) {
    const wanted = new Set(only.map((s) => s.toLowerCase().replace(/\s+/g, ' ')));
    const missing = only.filter((s) => !ops.has(s));
    if (missing.length) console.log(`[apis] 指定接口未在本地文档中找到：${missing.join(', ')}`);
    keys = keys.filter((k) => wanted.has(k.toLowerCase()));
    console.log(`[apis] 已限定只核对：${keys.join(', ') || '(无)'}`);
  }

  const candidates = [];
  for (const k of keys) {
    const { diffs, respChanges, addedParams, paramChanges } = apiMetaDiff(ops.get(k), remote.get(k));
    if (diffs.length) candidates.push({ key: k, diffs, respChanges, addedParams, paramChanges });
  }
  console.log(`[apis] 本地接口 ${ops.size} 个，Apifox 接口 ${remote.size} 个；待核对 ${candidates.length} 个`);
  if (!candidates.length) return { updated: 0, skipped: 0 };

  const max = Number(opts['max-apis'] || 50);
  let updated = 0;
  let skipped = 0;
  const skips = [];

  for (const c of candidates) {
    if (updated >= max) {
      console.warn(`[apis] 已达单次刷新上限 ${max}，剩余 ${candidates.length - updated} 个候选未处理（可用 --max-apis 调大）`);
      break;
    }
    const listed = remote.get(c.key);
    const op = ops.get(c.key);
    let detail;
    try {
      detail = await request(`${base}/api/v1/projects/${project}/http-apis/${listed.id}`, { headers, timeout });
    } catch (err) {
      skipped++; skips.push(`${c.key}：读取失败 ${err.message}`); continue;
    }
    if (detail.status !== 200 || !detail.json?.success) {
      skipped++; skips.push(`${c.key}：GET HTTP ${detail.status}`); continue;
    }
    const cur = detail.json.data || {};
    if (!cur.id) { skipped++; skips.push(`${c.key}：返回结构异常`); continue; }

    const changed = new Map(c.respChanges.map((r) => [r.code, r.desc]));
    const responses = (cur.responses || []).map((r) => (
      changed.has(String(r.code)) ? { ...r, description: changed.get(String(r.code)) } : r
    ));

    // 已有参数原样保留，只在对应 in 下追加本地新出现的参数
    const parameters = JSON.parse(JSON.stringify(cur.parameters || {}));
    for (const p of c.addedParams || []) {
      if (!parameters[p.in]) parameters[p.in] = [];
      parameters[p.in].push(buildApifoxParam(p));
    }
    // 已存在参数的类型/说明/枚举按本地文档刷新（id、enable、example 等其它键不动）
    for (const pc of c.paramChanges || []) {
      const list = parameters[pc.in];
      if (!Array.isArray(list)) continue;
      const idx = list.findIndex((x) => x.name === pc.name);
      if (idx === -1) continue;
      const hit = list[idx];
      list[idx] = {
        ...hit,
        type: pc.want.type,
        description: pc.want.description || hit.description || '',
        schema: mergeParamSchema(hit.schema, pc.want.schema),
      };
    }

    // multipart/form-data 的示例按本地文档补（Apifox 侧是 requestBody.parameters[].example）。
    // 注意：schema.example 与参数级 example 冲突，残留会让写入被忽略，所以顺手清掉。
    let requestBody = cur.requestBody;
    if (requestBody && (c.formChanges || []).length) {
      const want = new Map(c.formChanges.map((f) => [f.name, String(f.value)]));
      requestBody = {
        ...requestBody,
        parameters: (requestBody.parameters || []).map((x) => {
          if (!want.has(x.name)) return x;
          const schema = { ...(x.schema || {}) };
          delete schema.example;
          return { ...x, example: want.get(x.name), schema };
        }),
      };
    }

    const body = JSON.stringify({
      name: op.name || cur.name,
      method: cur.method,
      path: cur.path,
      tags: cur.tags,
      description: op.description || cur.description || '',
      parameters,
      requestBody,
      responses,
      status: cur.status,
      auth: cur.auth,
      securityScheme: cur.securityScheme,
      moduleId: cur.moduleId,
      folderId: cur.folderId,
    });
    const put = await request(`${base}/api/v1/projects/${project}/http-apis/${listed.id}`, {
      method: 'PUT', body, headers, timeout,
    });
    if (put.status < 200 || put.status >= 300 || !put.json?.success) {
      skipped++;
      skips.push(`${c.key}：PUT HTTP ${put.status} ${put.json?.errorMessage || put.text.slice(0, 120)}`);
      continue;
    }
    updated++;
    console.log(`[apis] 已更新 ${c.key}：${c.diffs.join('；')}`);
  }

  for (const s of skips) console.warn(`[apis] 跳过 ${s}`);
  console.log(`[apis] 完成：更新 ${updated} / 无需变更 ${candidates.length - updated - skipped} / 跳过 ${skipped}`);
  return { updated, skipped };
}

// ---------------------------------------------------------------------------

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) { process.stdout.write(USAGE); return; }
  const opts = { ...DEFAULTS, ...args };
  const timeout = Number(opts.timeout || 180000);
  if (!Number.isFinite(timeout) || timeout <= 0) throw new Error('--timeout 必须是正数');

  const file = path.resolve(opts.file);

  // ---------------- 阶段一：生成 ----------------
  let generated = null;
  if (args['verify-only']) {
    console.log('[1/2] 生成：跳过（--verify-only 只做只读校验）');
  } else if (args['no-generate']) {
    console.log('[1/2] 生成：跳过（--no-generate），直接用现有文件同步');
  } else {
    generated = runGenerate(opts);
  }
  if (args['generate-only']) {
    console.log('[1/2] 完成（--generate-only），未同步到 Apifox');
    return;
  }

  // ---------------- 阶段二：同步 ----------------
  if (generated && generated.file !== file) {
    throw new Error(`生成的产出是 ${generated.file}，但要同步的是 ${file}；两者必须一致`);
  }

  const projectId = String(opts.project || '').trim();
  if (!/^\d+$/.test(projectId)) {
    throw new Error('缺少 Apifox 项目 ID：请传 --project <id> 或设置 APIFOX_PROJECT_ID。'
      + '脚本不内置默认项目，防止把文档导错项目。');
  }
  opts.project = projectId;

  const token = args.token || process.env.APIFOX_ACCESS_TOKEN || readAccessToken(opts.config);
  if (!token) throw new Error('缺少访问令牌');
  const tokenSource = args.token ? '命令行参数' : process.env.APIFOX_ACCESS_TOKEN ? '环境变量' : opts.config;
  console.log(`[token] 来源 ${tokenSource}，前缀 ${token.slice(0, 4)}***（不显示完整值）`);

  const base = opts['api-base-url'].replace(/\/+$/, '');
  const project = encodeURIComponent(opts.project);
  const h = buildHeaders(opts, token);

  // 只读校验：确认令牌有效且项目 ID 正确，避免把文档导到错误项目。
  const check = await request(`${base}/api/v1/projects/${project}`, { headers: h, timeout });
  if (check.status !== 200 || !check.json?.success) {
    const msg = check.json?.errorMessage || check.json?.message || check.text.slice(0, 200);
    throw new Error(`项目校验失败 HTTP ${check.status}：${msg}`);
  }
  const projectName = check.json.data?.name || '(未命名)';
  console.log(`[check] 项目 ${opts.project} = ${projectName}（team ${check.json.data?.teamId ?? '-'}）`);

  if (args['verify-only']) { console.log('[verify-only] 校验完成，未导入'); return; }

  if (!fs.existsSync(file)) throw new Error(`找不到 OpenAPI 文件 ${file}`);
  const buf = fs.readFileSync(file);
  if (!buf.length) throw new Error(`OpenAPI 文件为空：${file}`);
  const yaml = buf.toString('utf8');
  if (!/^openapi:\s*["']?3/m.test(yaml) && !/^swagger:\s*["']?["']?2/m.test(yaml)) {
    console.warn('[warn] 文件中未检测到 openapi: 3.x / swagger: 2.x 声明，仍继续导入');
  }

  // 守卫：同步阶段必须上传「生成阶段刚产出的那份字节」，中间不得被改动。
  const hash = sha256(buf);
  if (generated && hash !== generated.hash) {
    throw new Error('openapi.yaml 在生成后被改动'
      + `（sha256 ${generated.hash.slice(0, 12)}… → ${hash.slice(0, 12)}…）：\n`
      + '  本流程要求文档只能由 swag 生成，同步阶段不得手工修改。请重新生成后再同步。');
  }
  console.log(`[2/2] 同步 ${file}（${buf.length} 字节，sha256 ${hash.slice(0, 12)}…）`);
  console.log('[guard] 同步阶段只读取该文件：脚本不写入、不重排、不补字段、不格式化');

  const body = JSON.stringify({ importFormat: 'openapi', data: yaml, targetBranchId: 0 });
  const refresh = !args['no-refresh-schemas'];
  if (args['dry-run']) {
    console.log(`[dry-run] 请求体 ${Buffer.byteLength(body)} 字节，未发送`);
    if (refresh) {
      const local = parseLocalSchemas(yaml);
      console.log(`[dry-run] 本地解析到模型 ${local.size} 个，正式执行时会核对并刷新其中与 Apifox 有差异的模型`);
    }
    if (!args['no-refresh-apis']) {
      const ops = parseLocalOperations(yaml);
      console.log(`[dry-run] 本地解析到接口 ${ops.size} 个，正式执行时会核对并刷新其中与 Apifox 有差异的名称/说明/响应说明/参数定义`);
    }
    return;
  }

  console.log(`[import] POST ${base}/api/v1/projects/${project}/import-data`);
  const result = await request(`${base}/api/v1/projects/${project}/import-data`, {
    method: 'POST', body, headers: h, timeout,
  });
  if (result.status < 200 || result.status >= 300 || !result.json?.success) {
    const detail = result.json?.errorMessage || result.json?.errorCode || result.text.slice(0, 300);
    throw new Error(`导入失败 HTTP ${result.status}：${detail}`);
  }

  const data = result.json.data || {};
  const api = sumCollection(data.apiCollection);
  const schema = sumCollection(data.schemaCollection);
  console.log('[result] 导入成功');
  console.log(`  接口:     ${fmt(api)}`);
  console.log(`  数据模型: ${fmt(schema)}`);
  let otherErrors = 0;
  for (const [name, coll] of Object.entries(data)) {
    if (name === 'apiCollection' || name === 'schemaCollection') continue;
    const c = sumCollection(coll);
    if (c.create || c.update || c.delete || c.error) {
      console.log(`  ${name}: ${fmt(c)}`);
    }
    otherErrors += c.error;
  }
  if (api.delete > 0 || schema.delete > 0) {
    console.warn('[warn] 本次导入删除了资源，请核对 Apifox 项目内容');
  }
  console.log('[note] 默认不删除未匹配接口；已有接口按后端策略保留或跳过（update=0 属正常）。');
  if (api.error || schema.error || otherErrors) {
    throw new Error('导入返回了错误计数，请检查 Apifox 项目');
  }

  // 导入不会可靠地刷新已有模型，这里补一次精确核对与更新。
  if (refresh) {
    const local = parseLocalSchemas(yaml);
    if (!local.size) {
      console.warn('[schemas] 未能从文件中解析出 components.schemas，跳过模型刷新');
    } else {
      const only = String(opts.schemas || '').split(',').map((s) => s.trim()).filter(Boolean);
      try {
        const r = await refreshSchemas({ base, project, headers: h, token, timeout, local, opts, only });
        if (r.updated) console.log('[note] 模型已按本地文件更新；下次重生成文档后无需手动补 PUT。');
      } catch (err) {
        console.warn(`[warn] 模型刷新失败（接口已导入成功）：${err.message}`);
      }
    }
  }

  // 导入不会更新「已存在」接口的元数据（名称/说明/响应说明/参数定义），这里同样补一次。
  if (!args['no-refresh-apis']) {
    const ops = parseLocalOperations(yaml);
    if (!ops.size) {
      console.warn('[apis] 未能从文件中解析出 paths，跳过接口元数据刷新');
    } else {
      const only = String(opts.apis || '').split(',').map((s) => s.trim()).filter(Boolean);
      try {
        await refreshApis({ base, project, headers: h, timeout, ops, opts, only });
      } catch (err) {
        console.warn(`[warn] 接口元数据刷新失败（接口已导入成功）：${err.message}`);
      }
    }
  }

  // 收尾复核：确认同步过程没有反过来写回本地文档。
  const after = sha256(fs.readFileSync(file));
  if (after !== hash) {
    console.warn(`[warn] openapi.yaml 在同步期间发生了变化（${hash.slice(0, 12)}… → ${after.slice(0, 12)}…），`
      + '不是本脚本所为，请检查是否有其它工具在监听该文件');
  }
}

main().catch((err) => {
  console.error(`[error] ${err?.message || err}`);
  process.exit(1);
});
