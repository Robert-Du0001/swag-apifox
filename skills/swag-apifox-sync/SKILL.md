---
name: swag-apifox-sync
description: 两步流水线：先用 swag-apifox 生成 docs/openapi.yaml，再把它原样同步到 Apifox 项目（接口 + 数据模型）。当用户提出「更新 Apifox 文档」「同步接口文档到 Apifox」「把 openapi.yaml 传上去」「重新导入 Apifox」「Apifox 项目文档同步」「改了 swag 注解 / example 之后推一下文档」，或任何需要重新生成 OpenAPI 再上传的场景时使用。**硬规则：顺序固定为「先生成、后同步」，同步阶段绝不修改 openapi.yaml 的任何内容（不手改、不后处理、不补 example、不格式化）。** 也用于排查 apifox CLI 全部命令 400、令牌失效、项目 ID 不对、导入 422、模型字段没刷新、接口说明或新参数没刷新、文档被人手改导致同步中止等问题。
agent_created: true
---

# swag → Apifox 两步流水线

## 铁律（先看这段，任何情况都不例外）

1. **顺序固定**：① 用 `swag-apifox` 生成 `docs/openapi.yaml` → ② 把这份文件原样同步到 Apifox。不允许跳过 ①、不允许先改 yaml 再同步。
2. **同步阶段对 `openapi.yaml` 零写入**：不手改、不用 `sed`/`yq`/脚本重写、不补 `example`、不调格式、不做任何后处理。文档内容**只能**来自 swag 生成。
3. **文档有问题（缺 example、字段类型错、tag 放错层）→ 回去改 Go DTO / swag 注解，重新生成，再同步。** 永远不要在 yaml 上打补丁。
4. 这条规则由脚本强制执行：生成结束会记录文件 sha256，同步前重新校验；中间被改过就**直接中止**并提示重新生成。

## 一条命令跑完两步（默认动作）

```bash
node "$SKILL_DIR/scripts/pipeline.mjs" --project <你的 Apifox 项目 ID>
```

`$SKILL_DIR` 指本技能目录（例如 `~/.workbuddy/skills/swag-apifox-sync`）；Windows 下把它换成带盘符的绝对路径。

在项目根目录执行即可。**项目 ID 必须显式传入**（`--project <id>` 或环境变量 `APIFOX_PROJECT_ID`），
脚本不内置默认项目，避免把文档导到别的项目。其它默认值：文件 `docs/openapi.yaml`、
生成命令 `swag-apifox build --color never -- --parseDependency --parseDepth 1`。

## 默认行为：改完就同步，不要问

若团队已约定「改了注解自动同步」，则：

- 只要本轮改过 DTO / swag 注解 / 路由，**直接跑整条流水线**，不要再问「要不要同步」。
- 跑完用一两句话汇报：接口计数、数据模型计数、刷新了哪些模型。
- 只有这三种情况才停下来找用户确认：`delete > 0`、`error > 0`、项目名与预期不符。
- 不需要跑：本轮只改业务逻辑、没动文档相关内容（那也不必生成）。

## 需要拆开执行时

| 目的 | 命令 |
| --- | --- |
| 只生成、不联网 | `... pipeline.mjs --generate-only` |
| 只同步已有文件、不生成 | `... pipeline.mjs --no-generate` |
| 只校验令牌与项目 ID | `... pipeline.mjs --verify-only` |
| 生成后试运行、不发请求 | `... pipeline.mjs --dry-run` |
| 换项目 | `... pipeline.mjs --project <id>` |
| 换生成参数 | `... pipeline.mjs --swag-args "--parseDependency --parseDepth 1"` |
| 指定项目根 | `... pipeline.mjs --root /path/to/project` |

注意 `--generate-only` 只适合检查生成是否成功；**正常交付必须跑完整两步**。

## 输出怎么读

```
[1/2] 生成：swag-apifox build --color never --quiet -- --parseDependency --parseDepth 1
[1/2] 产出 .../docs/openapi.yaml（667691 字节，153 个路径 / 202 个操作，sha256 badaa19612cb…）
[token] 来源 ...config.toml，前缀 afxp***（不显示完整值）
[check] 项目 123456 = 示例项目（team 789）
[2/2] 同步 .../docs/openapi.yaml（667691 字节，sha256 badaa19612cb…）
[guard] 同步阶段只读取该文件：脚本不写入、不重排、不补字段、不格式化
[result] 导入成功
  接口:     新增 0 / 更新 0 / 忽略 202 / 删除 0 / 错误 0
  数据模型: 新增 0 / 更新 0 / 忽略 482 / 删除 0 / 错误 0
[schemas] 本地模型 493 个，Apifox 模型 482 个；待核对 1 个
[schemas] 已更新 dto.XxxResponse：新增 purchased_bytes；删除 total_mb
[apis] 本地接口 202 个，Apifox 接口 202 个；待核对 1 个
[apis] 已更新 delete /companies/{company_id}/attachments/dirs：接口说明；响应 409 说明
```

汇报时明确这几点：

- `delete = 0` 是安全预期，Apifox 上手工维护的接口不会被删；
- `update = 0` 属正常（import 对已存在接口一律忽略），接口名称/说明/响应说明/参数定义由后面的 `[apis]` 阶段补齐；
- `[schemas] 已更新 ...` 要原样转述，说明哪个模型加了/删了哪些字段；
- `[apis] 已更新 ...` 要转述，说明哪个接口的说明变了（注解里的 `@Description` / `@Failure` 文案改了就靠这一步落地）；
- `[schemas] 跳过 ...` 表示该模型结构无法安全自动重建，需要人工确认（不要擅自改成全量覆盖）。

## 模型刷新已内置，不用手动 PUT

导入后脚本自动做四件事：

1. 拉全量模型列表（`api-schemas?perPage=500`，按 `meta.total` 翻页，最多 10 页）；
2. 解析本地 `components.schemas`（属性名/顺序/必填/说明/类型/示例/枚举），先快速比对筛候选；
3. 候选逐个 `GET /api/v1/api-schemas/{id}`，以单条为准（列表接口有缓存）；
4. 有差异才 `PUT`，**最小改动**：只增删属性、改说明/类型/示例/枚举（`enum` 与 `x-apifox-enum`）/必填/顺序，已有属性的引用、格式等原样保留。

单次最多刷新 30 个模型（`--max-schemas` 可调）；超出会中止并提示，避免误批量改写。

**数组属性的示例（已修复的拆平 bug，2026-11-24）**：Apifox 模型用 `examples` 存示例列表，
**每个元素是列表里的一个独立示例**，所以数组示例本身必须作为整体元素——`example: [1,2]`
（array 类型的一个示例）应写 `examples: [[1,2]]`。旧版脚本误写成 `examples: [1,2]`，
导致 Apifox 把 1、2 当成两个独立示例：字段类型是 `array[integer]`，示例却显示成 `integer`
（触发场景：应收应付余额详情的 `attachment_ids`）。已在 `pipeline.mjs` 的 `quickDiffers`
（快速比对）、`buildPropValue`（新增属性）、`planUpdate`（刷新属性）三处统一按 `[spec.example]`
包装；线上受影响的模型也已随流水线自动修正。复核方式：`--schemas <模型名>` 定向重跑，
日志 `[schemas] 待核对 0` 即通过。

## 接口元数据刷新也已内置

`import-data` 对**已存在**的接口只会「忽略」，所以改了 `@Summary` / `@Description` / `@Failure`
的文案，导入不会更新线上的接口。脚本导入后补一轮：

1. 解析本地 `paths`（method + path、summary、description、各状态码 description、parameters）；
2. 拉 Apifox 接口列表比对（列表项本身就带这些字段，零额外请求）；
3. 只对有差异的接口 `GET` 单条后 `PUT`：改名称/说明/响应说明，**追加本地新出现的参数**，
   并**刷新已存在参数的定义**（见下）；请求体、普通响应 `$ref`、参数 `id`/`enable` 等结构原样传回（多态响应除外，见下）；
4. **只补不清（针对资源，不针对字段）**：本地 `@Description` 为空时不会把线上已有的说明冲掉；
   本地文档里没有的线上参数保留；参数 `title` 等未管理字段原样保留，`example` 则按本地有值才覆盖。

### 同一状态码多种返回（`oneOf` 响应结构）

一个接口有时需要表达「成功时有数据 / 没数据时 `data: null`」这类多形态返回。**直接在控制器上写两条
`@Success 200`，不要往描述里手写「未购买时 data 为 null」**：

```go
//	@Success  200 {object} dto.OkResponse[dto.AttachmentSpaceResponse] "获取成功"
//	@Success  200 {object} dto.NullResponse "获取成功"
```

swag 会把同状态码合并成一个 `oneOf`（两个 schema 引用），description 取**最后一条**——
所以两条描述建议写成一样的，避免整体描述变成误导性的「未购买」。

`import-data` 不会更新已有接口的响应 schema，所以 2026-11-25 起 `[apis]` 阶段会额外同步这一类：
解析各响应的 `oneOf` 引用 → 用模型名到 Apifox 定义 ID 的映射改写成 `$ref: '#/definitions/<id>'`
→ 与线上 `jsonSchema.oneOf` 比对，不同才写回。只处理引用数 ≥2 的 `oneOf`，其它响应结构一律不动；
模型未找到映射时跳过并告警。日志形如 `[apis] 已更新 xxx：响应 200 结构（A / B）`，
再跑一次应为 `更新 0 / 无需变更 N`。复核可直接读接口详情的 `responses[].jsonSchema.oneOf`。

**响应示例（2026-11-25）**：swag v2 的 `example(...)` 只作用于参数，不会生成响应示例；
Apifox 把示例存在接口对象的 `responseExamples`（**不在** `responses[]` 里），`import-data`
也不会给已有接口补。因此 `[apis]` 阶段对同一批 `oneOf` 响应再按本地 `components.schemas`
递归拼出每个分支的示例 JSON：属性 `example` 优先，`$ref` 递归展开，`type: 'null'` → `null`，
没有示例的按类型给默认值（`string`/`0`/`false`/`[]`/`{}`）。写入时按 `oasKey`（模型名）对齐、
**只补不清**，手工加的其它示例不会被删；示例名就是模型名，如
`dto.OkResponse-dto_AttachmentSpaceResponse`（有数据）与 `dto.NullResponse`（`data: null`）。
日志形如 `[apis] 已更新 xxx：响应 200 示例 2 个`；复核读接口详情的 `responseExamples[].data`。

### 已存在参数的差异检测（`[apis]` 第 3 步）

按 `in + name` 匹配本地与线上参数，比对并刷新四类差异：

| 字段 | 行为 |
|---|---|
| `type` | 以本地为准（`ids` 从 `array` 改成 `string` 这种就是靠它落地） |
| `description` | 仅当本地写了非空文案时才覆盖，空描述不动线上 |
| `schema.enum` / `schema['x-apifox-enum']` | 本地有则写入，本地没有则**删除**线上残留的枚举（swag 曾误生成 `enum: [100, 2]`，靠这条清掉） |
| `schema.example` | 本地写了 `example(...)` 或数组的 `extensions(x-example=[...])` 才覆盖；本地没有则保留线上值 |

刷新时只覆盖这几类键，参数 `id`、`enable` 等其它键全部保留；
参数 `required` 暂不比对（线上可能有人为调整，需要时再加）。

### 接口级 enum（`oasExtensions`，operation 误判清理）

operation 节点（有 `responses`）的 `description` 里如果出现 ≥2 个「数字+中文」组合
（如 `code=200 成功，code=400 订单不存在`），旧版 `swag-apifox` 的 enum transformer 会
误把它当成枚举，在 operation 上生成 `enum` + `x-apifox-enum`，导入 Apifox 后落在接口的
`oasExtensions`（JSON 字符串）里，Apifox 会在接口标题下方渲染出一个莫名其妙的 enum 框。

- 治本：`swag-apifox` 的 enum transformer 已跳过 operation 节点（`internal/transform/apifox_enum.go`
  的 `isOperationNode`），新生成的文档不会再有这类 enum。
- 清理：`[apis]` 会比对本地 operation 级 enum 与线上 `oasExtensions.enum`，
  本地没有就删掉线上的 `enum` / `x-apifox-enum`（其它扩展键原样保留）。
  日志形如 `[apis] 已更新 get /x：扩展 enum（线上 [200,400] → 本地 []）`。

### 空响应名补中文（409 等，2026-11-25）

Apifox 对部分状态码内置了中文响应名（200 成功、400 请求有误、401 没有权限、500 服务器错误、502 网关错误），
但 **409 没有内置名**，`responses[].name` 是空串，导致 Mock 列表和「返回响应」tab 上 409 显示为空白。
`[apis]` 阶段现在会在本地 `@Failure` 的 description 非空、线上 `name` 为空时，把 description 写进 `name`（只补不清，
已有名称一律不动）。日志形如 `[apis] 已更新 post /x：响应 409 名称`。
因为 swag 把同状态码多条 `@Failure` 合并成 oneOf、description 取最后一条，想让 409 名称简短，
就给多条 409 写**相同且简短**的文案（如都写「业务校验失败」），详细说明放到响应 DTO 的 `@Description` 里。

**multipart/form-data 是另一套结构**：Apifox 把表单参数放在
`requestBody.parameters[]`，示例字段是参数级 `example`（字符串），而不是 `schema.example`。
本地取值来自 swag 输出的 `requestBody.content['multipart/form-data'].schema.properties.<name>.example`，
两者不一致时只改该参数并顺手删掉 `schema.example` 残留（两者并存会让写入被忽略）。

**数组参数走 `extensions(x-example=[...])`**：swag 的 `example()` 不支持数组类型
（`defineType` 对 array 直接返回错误），但参数扩展会被原样保留在 `schema` 里，
`swag-apifox` 的 `example` transformer 再把 `x-example` 改名成 `example` 并展开成 YAML 列表。
所以 `[]string` 参数（如日期区间 `date`）这样写：

```go
//	@Param date query []string true "日期区间，按顺序传 2 个值：开始日期、结束日期" collectionFormat(multi) extensions(x-example=[2026-10-01,2026-10-31])
```

产出的是标准 `schema.example: ['2026-10-01', '2026-10-31']`，脚本按普通示例比对，
**不需要也不允许**再到 Apifox 手工补。

新增参数会带上 `type` / `enum` / `required`，枚举说明从描述里的「0-全部，1-已关联」还原成
Apifox 的 `x-apifox-enum`。实测日志：

```text
[apis] 已更新 get /companies/{company_id}/attachments：参数 query:ids（类型 array→string）
[apis] 已更新 get /companies/{company_id}/attachments：参数 query:relation_status（枚举、枚举说明）
[apis] 已更新 post /companies/{company_id}/attachments：formData:dir_id 示例
```

单次最多 50 个接口（`--max-apis` 可调）。这一步失败只告警，不影响已完成的导入。

## 环境前置 URL 同步（`[env]`）

权威数据源是 `openapi.yaml` 的 `servers` 块——由 Go 注解 `@servers.url` / `@servers.description`
生成，`description` 就是 Apifox 环境名。例如：

```yaml
servers:
- description: 本地开发
  url: http://192.168.2.170:3002/api
- description: 测试环境
  url: http://192.168.2.100:3006/api
```

脚本导入后自动：

1. 解析本地文档的 `servers`，按 `description` 匹配环境名（支持包含匹配；单条无 description 时作为全局默认）；
2. 拉全量环境列表，只处理 `type=normal` 的普通环境（Mock 环境由 Apifox 自管，不动）；
3. 把匹配到的 URL 写入该环境的 `baseUrl` 和 `baseUrls` **全部键**（含各模块键、`"0"`、`"default"`），
   与接口 `moduleId` 无关——OpenAPI 导入新建的 `moduleId=0` 接口也能取到前置 URL；
4. 有变更才 `PUT`；`--no-refresh-env` 可关闭这一步。只同步 `servers` 明确声明的环境，不猜其它环境。

本地文档没有 `servers` 时兜底：沿用「从环境已有模块 URL 复制」，只补 `baseUrls["0"]` 和
`baseUrls["default"]` 的空缺，不覆盖已有值。

日志形如 `[env] 本地开发 按 servers 同步前置 URL（baseUrl、8848517、0、default = http://...）`，
幂等运行时是 `[env] 完成：更新 0 / 跳过 0`。这一步失败只告警，不影响已完成的导入。

## 注解补示例的约定

query / formData 参数一律写 `example(...)`，否则同步到 Apifox 就是空的示例值：

```go
//	@Param    limit query integer true "每页显示条数" example(20)
//	@Param    bus_type formData int false "关联单据类型" Enums(1, 2, 3) example(1)
```

- 示例值不要包含空格；
- `file` 类型（`format: binary`）的 `example` 会被 swag 丢弃，写了没有效果；
- `[]string` 数组参数用 `extensions(x-example=[a,b])`，`example(...)` 对数组无效；
  方括号必须写，它的作用是不让 swag 的 `splitNotWrapped` 把逗号当成扩展分隔符。

## 参数速查

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-p, --project <id>` | 必填：`APIFOX_PROJECT_ID` 或 `--project` | Apifox 项目 ID，脚本不内置默认值 |
| `-f, --file <path>` | `docs/openapi.yaml` | 同步的文档路径，必须与生成产出一致 |
| `--root <dir>` | 从当前目录向上找 `.swaggo` / `go.mod` | 生成阶段的工作目录 |
| `--swag-cmd <name>` | `swag-apifox` | 生成命令，可给绝对路径 |
| `--swag-args <args>` | `--parseDependency --parseDepth 1` | 透传给 swag 的参数 |
| `--no-generate` / `--generate-only` | — | 关掉阶段一 / 只要阶段一 |
| `--quiet` / `--keep-swagger` | — | 静默 swag 输出 / 保留 `docs/swagger.yaml` |
| `-c, --config <path>` | `~/.apifox/config.toml` | Apifox 令牌配置 |
| `--token <value>` | — | 不推荐，会进进程列表 |
| `--api-base-url <url>` | `https://api.apifox.com` | 私有化部署时覆盖 |
| `--cli-version` / `--api-version` | `2.2.9` / `2026-05-28` | 两个必需的版本头 |
| `--timeout <ms>` | `180000` | 单次请求超时 |
| `--refresh-schemas` / `--no-refresh-schemas` | 开启 | 导入后是否刷新数据模型 |
| `--schemas <a,b>` | — | 只核对指定模型 |
| `--max-schemas <n>` | `30` | 单次最多 PUT 多少模型 |
| `--refresh-apis` / `--no-refresh-apis` | 开启 | 导入后是否刷新接口名称/说明/响应说明/参数定义 |
| `--apis <list>` | — | 只核对指定接口，格式 `"get /path,post /path"` |
| `--max-apis <n>` | `50` | 单次最多 PUT 多少接口 |
| `--refresh-env` / `--no-refresh-env` | 开启 | 按 openapi.yaml 的 servers 同步环境前置 URL |
| `--verify-only` / `--dry-run` | — | 只读校验 / 生成但不发请求 |

环境变量优先级：令牌 `--token` > `APIFOX_ACCESS_TOKEN` > 配置文件；
项目 ID `--project` > `APIFOX_PROJECT_ID`（两者都没有就直接报错，不再回退到某个固定项目）。

## 前置条件

- Node.js 18+（脚本只用内置模块，无第三方依赖）。
- `swag-apifox` 在 PATH 里；否则用 `--swag-cmd` 指定绝对路径（安装方式见仓库 README）。
- Apifox 已登录：`~/.apifox/config.toml` 有 `access_token`，或设 `APIFOX_ACCESS_TOKEN`。
  未登录时引导用户自己执行 `apifox auth login --with-token <令牌>`。
  **不要索取令牌明文，不要把令牌写进仓库或对话。**

## 安全约定

- 令牌只在内存里传递，脚本只打印前 4 位和来源，不写任何文件。
- 默认不删除未匹配接口、不强制覆盖已有接口。**未经用户明确确认，不要加 `deleteUnmatchedResources` 或任何全量覆盖参数。**
- 模型刷新是逐模型、最小改动的外部写，只在与本地文件确有差异时才 PUT，不会删接口/删模型。
- 导入是不可逆的外部写操作；执行前脚本会先校验项目名，项目里的手工内容一律保留。
- 导入前备份拿不到（导出端点返回空，见 `references/apifox-api.md`），所以更要靠 `delete = 0` 核对。
- 代码改动留在工作区，由使用者自己决定何时提交；不要代为 `git add` / 提交。

## 排错

- **「文档在生成后被改动」**：说明中间有人手改/格式化了 `openapi.yaml`。删掉改动、重新跑整条流水线；
  如果是文档内容确实要变，回去改 Go 注解，不要改 yaml。
- **生成失败**：直接看 swag 的报错（脚本已透传）。先修注解/类型，再重跑。
- **`找不到生成命令`**：`swag-apifox` 不在 PATH，用 `--swag-cmd` 指定绝对路径。
- **模型字段「类型 `array[xxx]`，示例却是单个值」**：数组示例被拆平了，`examples` 里应是
  `[[...]]` 而不是 `[...]`。检查本地 `example:` 是否是数组，再确认 `pipeline.mjs` 三处
  示例包装用的是 `[spec.example]`（详见「模型刷新已内置」的「数组属性的示例」）；
  重跑整条流水线即可自动修正线上模型，不要去 Apifox 手工改示例。
- **CLI 400 / 导入 422 / 模型没刷新 / 项目名不对**：读 `references/apifox-api.md`，里面有请求头、
  分页参数、计数结构、缓存问题、单模型 PUT 结构的实测记录。

## 资源

- `scripts/pipeline.mjs` — 唯一入口：生成 → 校验 sha256 → 导入 → 刷新模型 → 收尾复核。
- `references/apifox-api.md` — API 端点、请求头、响应结构、排错记录。

## 安装与更新

本技能随 `swag-apifox` 仓库的 `skills/swag-apifox-sync/` 分发。安装或升级时，把该目录**整个**复制到 WorkBuddy 的技能目录：

```sh
# macOS / Linux
cp -r skills/swag-apifox-sync ~/.workbuddy/skills/

# Windows PowerShell
Copy-Item -Recurse -Force skills\swag-apifox-sync "$env:USERPROFILE\.workbuddy\skills\"
```

重启 WorkBuddy 会话（或刷新技能列表）后即可使用。若只在某个项目里共享，把目录放到该项目根目录的 `.workbuddy/skills/` 下，作为项目级技能。

维护者改动技能后，把改动同步回仓库的 `skills/swag-apifox-sync/` 并提交；其他人更新仓库后重新安装即可。技能文件里不含任何 Apifox 令牌或固定项目 ID，项目 ID 由使用者在运行时显式提供。
