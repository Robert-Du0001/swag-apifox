# Apifox 同步 API 参考与排错记录

本文件记录 2026-11-12 在 Windows / Git Bash 环境下实测验证过的事实，2026-11-16 补充两步流水线
（`scripts/pipeline.mjs`）相关记录，供排错和扩展脚本时参考。

## 两套 API，不要混用

| 用途 | 路径前缀 | 认证 | 说明 |
| --- | --- | --- | --- |
| CLI 内部 API | `https://api.apifox.com/api/v1/...` | `Authorization: Bearer <afxp...>` + `X-apifox-cli-version` + `X-apifox-Api-Version` + `X-Project-Id` | 本技能使用，实测可用 |
| 开放 API | `https://api.apifox.com/v1/...` | `Authorization: Bearer <APS-...>` + `X-Apifox-Api-Version: 2024-03-28` | 官方文档的 `POST /v1/projects/{projectId}/import-openapi` 属于这一套 |

用 `afxp` 令牌请求 `/v1/...`（无 `/api`）会得到 302 跳转到 `/help/index.html`，这是路径/令牌体系不匹配，不是令牌失效。

## 令牌

- 位置：`~/.apifox/config.toml`（Windows 为 `C:/Users/<用户>/.apifox/config.toml`）。
- 结构：`[account:<URL编码的 base|userId>]` 段内存 `access_token`、`api_base_url`、`user_id` 等；`[auth]` 段的 `current_account` 指向当前账号。
- 令牌前缀实测为 `afxp`（长度 41），由 `apifox auth login --with-token <令牌>` 写入。
- 生成位置：Apifox 右上角头像 → 账号设置 → API 访问令牌。
- 令牌不要写进仓库、不要在输出里打印完整值；脚本只打印前 4 位。

## 已验证端点

### 校验项目（只读）

```
GET https://api.apifox.com/api/v1/projects/{projectId}
Headers: Authorization / X-apifox-cli-version / X-apifox-Api-Version / X-Project-Id
→ 200 {"success":true,"data":{"id":123456,"name":"示例项目","teamId":789,"..."}}
```

### 导入 OpenAPI

```
POST https://api.apifox.com/api/v1/projects/{projectId}/import-data
Headers:
  Authorization: Bearer <token>
  X-apifox-cli-version: 2.2.9
  X-apifox-Api-Version: 2026-05-28      ← 注意不是开放 API 文档的 2024-03-28
  X-Project-Id: {projectId}             ← 缺失会 422 "Parameter is missing"
  Content-Type: application/json
Body:
  {"importFormat":"openapi","data":"<OpenAPI YAML 全文字符串>","targetBranchId":0}
→ 200 {"success":true,"data":{"apiCollection":{"item":{"createCount":...}}, ...}}
```

- `data` 是文件内容**原样字符串**（YAML 或 JSON 均可），不是文件路径、不是 base64、不是 URL。
- `importFormat` 直接写 `openapi`；CLI 源码里只有 `apifox` 会被映射成 `app`，其余原样透传。
- 请求体约 677 KB（对应约 600 KB 的 YAML），后端接受。

### 响应计数结构

`data` 下每个集合（`apiCollection`、`schemaCollection`、`docCollection` 等）含 `item` / `folder` / `service` / `api` 等对象，每个对象有：
`createCount`、`updateCount`、`ignoreCount`、`deleteCount`、`errorCount`。

实测一次「文档只新增了一个接口」的导入：
`apiCollection.item` = create 1 / update 0 / ignore 201 / delete 0 / error 0；
`schemaCollection.item` = create 3 / ignore 479；总接口数 202 与 `create + ignore` 吻合。

- `ignore` 通常表示已存在且无变化、或按现有策略跳过。
- `update = 0` 不代表失败；本流程**默认不强制覆盖已有接口**。
- `delete = 0` 是本技能的安全预期：不删除 Apifox 上未在文档里出现的接口（含手工维护内容）。若看到 delete > 0，立即核对项目。

### 查询接口列表（只读，用于核对）

```
GET https://api.apifox.com/api/v1/projects/{projectId}/http-apis?page=1&perPage=500
→ 200 {"success":true,"data":[{"id":...,"method":"put","path":"/companies/...","name":...}],"meta":{"total":202,"perPage":10,"totalPages":21}}
```

注意分页参数名是 **`perPage`**（不是 `pageSize`）；不传时每页 10 条。响应里 `path` 字段可直接用于核对新旧路径是否残留。

### 导出备份（当前不可用）

`GET /api/v1/projects/{id}/export-openapi?oasVersion=3.1&scope=all` 返回 200 但响应体为空；
`POST` 同端点返回 422 `Parameter is missing`。要拿到导入前备份需进一步摸清该端点的参数，**目前不要依赖它做回滚**。可改为在导入后用 `http-apis` 列表或 Apifox 自带的项目历史做核对。

## 已知坑：apifox CLI 2.2.9 全部命令 400

现象：`auth status` 显示已登录，但 `auth whoami`、`project list`、`project get`、`team list`、`export`、`import auto-import list` 一律返回
`{"success":false,...,"message":"Request failed with status code 400"}`，`whoami` 还提示「尚未登录」。

排查结论：
- 不是令牌失效：同一令牌直接请求 `GET /api/v1/user` 返回 200 并返回账号资料，`GET /api/v1/projects/{projectId}` 也返回 200。
- 不是网络：curl 走代理与 `--noproxy` 直连都能到达服务器。
- 因此是 CLI 版本与后端接口不兼容（CLI 发行于 2026-08，本机系统时间 2026-11）。
- 应对：不要反复重试 CLI 命令，直接用本技能的脚本走 HTTP；或者升级 CLI（`npm i -g apifox-cli@latest`）后重新验证。

## 已知坑：改了模型字段，导入后 Apifox 上没变

实测（2026-11-16）：

- 给 `dto.AttachmentCapacityResponse` 删掉 4 个 `*_mb` 字段后导入：模型 description 与 properties **都更新了**。
- 随后再给它加 `purchased_bytes` 后导入：返回同样 `success:true`、`schemaCollection.item` 仍是 `ignore`，但该模型 `updatedAt` 不变、属性里没有新字段——**已有模型是否随导入刷新并不稳定，不能依赖 `import-data` 保证模型同步**。

### 自动处理（脚本已内置，无需手动）

`pipeline.mjs` 在导入成功后自动跑一次模型核对与刷新，流程：

1. `GET /api/v1/api-schemas?page=N&perPage=500` 拉全量模型（按 `meta.total` 自动翻页，最多 10 页）。
2. 解析本地 `components.schemas`（只取属性名/顺序/必填/说明/类型/示例/枚举），先做「快速比对」筛出候选。
3. 候选逐个 `GET /api/v1/api-schemas/{id}` 单条取真实定义（列表可能有缓存，单条才准）。
4. `PUT /api/v1/api-schemas/{id}` 提交**最小改动**后的 `jsonSchema`。

最小改动原则（避免整体重建丢信息）：

- 新增属性：只从本地 YAML 重建 `description / type / examples / enum / x-apifox-enum / form`，`$ref` 按名称换成 `#/definitions/{id}`。
- 删除属性：本地没有的从 Apifox 删掉（这就是「文档即真相」对模型字段的落地）。
- 已有属性：克隆原对象，仅在类型、说明、示例、枚举值（`enum`/`x-apifox-enum`）确有差异时改这几项；引用、格式等其余字段原样保留。本地 `example:`（标量）或 `example:` 下的 `- ` 列表会转成 Apifox 的 `examples` 数组；本地没写示例时不会清空线上已有示例。只比对本地**有写**的示例/枚举，不会因本地没写而清空线上内容。
- 同步 `required` 与 `x-apifox-orders`，模型 `description` 有变化时一并更新。

会跳过（打印 `[schemas] 跳过 ...`，退出码仍 0）：属性含 `oneOf`/`additionalProperties` 等暂不支持重建的字段、引用目标在 Apifox 中不存在、单条 GET 或 PUT 失败、本地无 `properties` 的标量别名模型。单次最多 PUT 30 个模型（`--max-schemas`），超出会中止提示。

Go 注解坑（2026-11-18）：字段注释用多行 `+ 日常收支：1` 这种列表写枚举时，`swag-apifox` 生成的 `enum` / `x-apifox-enum` **只保留前 4 项**；改成单行 `1-日常收支，2-内部转账，...` 才会输出完整 8 项（见 `dto.AttachmentRelateItem.bus_type`）。脚本已比对枚举，改完注解重跑即可自动刷到 Apifox。

实测自愈（2026-11-16）：先把线上 `dto.AttachmentCapacityResponse` 故意改旧（删 `purchased_bytes`、加回 `total_mb`），再跑一次同步，输出

```
[schemas] 本地模型 482 个，Apifox 模型 482 个；待核对 1 个
[schemas] 已更新 dto.AttachmentCapacityResponse：新增 purchased_bytes；删除 total_mb；必填 ...
[schemas] 完成：更新 1 / 无需变更 0 / 跳过 0
```

单条 GET 复核：字段、required、orders、说明全部恢复正确。全程只动这 1 个模型，接口 202 个仍是 create 0 / update 0 / ignore 202 / delete 0。

核对时注意列表接口有缓存：

- `GET /api/v1/api-schemas?page=1&perPage=500` 拉全量列表，可能返回**旧数据**（实测返回 8 个字段，而单条查询已经是 4 个）。
- 以单条为准：`GET /api/v1/api-schemas/{schemaId}`，看 `data.jsonSchema.properties` 与 `data.updatedAt`。

单模型精确更新（只影响这一个模型，不碰接口、不删除任何东西）：

```
PUT https://api.apifox.com/api/v1/api-schemas/{schemaId}
Headers: 同 import-data（Authorization / X-apifox-cli-version / X-apifox-Api-Version / X-Project-Id）
Body（替换语义，字段要传全）:
  {
    "name": "dto.AttachmentCapacityResponse",
    "displayName": "",
    "jsonSchema": { "description": "...", "properties": {...}, "required": [...], "type": "object", "x-apifox-orders": [...] },
    "description": "...",
    "folderId": 0,
    "moduleId": <取自 GET 结果>,
    "visibility": "INHERITED"
  }
→ 200 {"success":true,...}，随后 GET 单条确认 updatedAt 与 properties
```

- `jsonSchema` 用 Apifox 自己的结构：每个属性是 `{"description":...,"type":"integer","examples":[0]}`，是 `examples` 数组而不是 OpenAPI 的 `example`；顶层带 `x-apifox-orders` 控制字段顺序。
- 不要回传只读字段：`id`、`projectId`、`creatorId`、`editorId`、`createdAt`、`updatedAt`、`ordering`。
- 因为 PUT 是整体替换，`folderId` / `moduleId` / `visibility` 必须一并带上，否则可能被清空。

## 两步流水线的硬约束（2026-11-16）

用户明确要求：「先用 swag-apifox 生成，再同步到 Apifox，同步的时候不要修改任何 openapi.yaml 的内容」。
落地方式：

1. `pipeline.mjs` 阶段一必须真的调用 `swag-apifox build`（不要手工 `swag init` 绕过后处理，
   否则 `x-nullable`、`x-apifox-enum` 不会生成）。
2. 生成结束立刻算 `sha256` → 同步前重新读文件再算一次 → 不一致就抛错退出（退出码 1）：
   `openapi.yaml 在生成后被改动（sha256 xxx… → yyy…）`。
3. 脚本对 yaml 只做 `fs.readFileSync`，没有任何写入路径；导入成功后还会再算一次 sha256，
   若变化只告警（说明有外部工具在写该文件）。
4. 文档内容的修改一律回到 Go 注释/ DTO 上做，重新生成再同步；不要在 yaml 里补 example 或改缩进。

实测（2026-11-16）：同一份代码连续跑两次 `--generate-only`，产出 sha256 完全一致
（667691 字节 / 153 个路径 / 202 个操作），说明生成是幂等的，改没改代码一眼能看出来。

## 删除单条接口（接口改名/改路径后的收尾）

导入只做 create / update / ignore，**从不删除**文档里消失的接口，所以接口一旦改了路径（例如
`DELETE .../dirs/{id}` 改成 `DELETE .../dirs`），Apifox 上会留一条幽灵接口。删掉它：

```
DELETE https://api.apifox.com/api/v1/projects/{projectId}/http-apis/{apiId}
Headers: 同 import-data（Authorization / X-apifox-cli-version / X-apifox-Api-Version / X-Project-Id）
→ 200 {"success":true,"data":null}
```

- 路径必须带 `/projects/{projectId}` 前缀；`DELETE /api/v1/http-apis/{id}`（不带项目）返回 **405 openresty**。
- `apiId` 从 `GET /api/v1/projects/{projectId}/http-apis?page=1&perPage=500` 里按 `method + path` 找。
- 只删单条、精确 id；不要用批量删除来「对齐文档与 Apifox」。

实测（2026-11-16）：目录删除接口从单条改成批量后，导入结果 `新增 1 / 忽略 201`，旧
`DELETE /companies/{company_id}/attachments/dirs/{id}`（id 535718097）仍在；用上面的 DELETE 删掉后
`http-apis` 总数从 203 回到 202，与文档一致。

## 更新单条接口（名称 / 说明 / 响应说明）

`import-data` 对已存在接口返回 `update=0 / ignore=全部`，**注解里改过的 `@Summary`、`@Description`、
`@Failure` 文案不会自己到达 Apifox**，必须单条 PUT：

```
PUT https://api.apifox.com/api/v1/projects/{projectId}/http-apis/{apiId}
Headers: 同 import-data，另加 Content-Type: application/json
Body（白名单，字段从 GET 单条结果取回原值，只替换要改的）:
{
  "name": "...", "method": "delete", "path": "/companies/{company_id}/attachments/dirs",
  "tags": [...], "description": "...", "parameters": {...}, "requestBody": {...},
  "responses": [ ...原样，仅替换目标 code 的 description... ],
  "status": "released", "auth": {...}, "securityScheme": {...},
  "moduleId": 0, "folderId": 99541545
}
→ 200 {"success":true,"data":null}
```

要点：

- **必须 GET 单条**再拼 body（列表接口有缓存，且列表项字段虽全但以单条为准）。
- `parameters` 是**对象**（`{path, query, cookie, header}`）不是数组；`responses` 是数组，每项带
  `id / code / jsonSchema.$ref / description`。只改 `description`，`$ref`、响应 id、文件夹归属
  （`folderId`）、`status` 原样传回，实测 PUT 后这些字段全部未变。
- 同样要带 `/projects/{projectId}` 前缀（`PUT /api/v1/http-apis/{id}` 未验证，别试）。
- 「只补不清」：本地描述为空时**不要** PUT 空字符串，否则会把在 Apifox 手工补的说明冲掉。
- 新增查询参数同理：`parameters` 是 `{path:[], query:[], cookie:[], header:[]}`。按 `in + name` 比对，
  已有参数对象整条原样传回（含 `id`），只在对应数组末尾追加新对象：
  `{id, name, required, enable:true, description, type, schema:{type, enum?, x-apifox-enum?}}`。
  `id` 给个唯一字符串即可；`x-apifox-enum` 可从 description 的「0-全部，1-已关联」还原。
  实测（2026-11-18）：新增 `relation_status` 后 import 仍 `update 0`，靠 PUT 补上，枚举名同步正确。

实测（2026-11-16）：把 409 说明从「data 为 DetailMsg」改成「msg 为 DetailMsg」后，重新生成 + 导入仍是
`update 0`，线上描述不变；故意把线上描述改坏，再跑 `--no-generate --no-refresh-schemas`，
`[apis] 待核对 1 个 → 已更新 ...：接口说明；响应 409 说明`，描述自动修回，参数/请求体/$ref/folder 均未受影响。

## 覆盖策略的边界

CLI 源码里「Apifox 格式」导入分支带有 `apiOverwriteMode`、`requestOverwriteBehavior`、`componentOverwriteBehavior` 等字段；`openapi` 分支只有上面三个字段。开放 API 的 `POST /v1/.../import-openapi` 支持 `options.endpointOverwriteBehavior`，其中 `deleteUnmatchedResources` 会删除未匹配接口。

**本技能不启用任何删除/全量覆盖选项。** 若用户明确要求「以文档为准强制覆盖已有接口」或「删除未匹配接口」，先确认影响范围，再单独调研对应参数，不要在默认流程里加上。
