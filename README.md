# swag-apifox

一个**独立的命令行应用**：读取 [swag v2](https://github.com/swaggo/swag/tree/v2) 生成的 OpenAPI 3.1 文档，按扩展规则做二次处理，产出可直接导入 [Apifox](https://apifox.com/) 的 YAML。

它位于 `Go 源码 → swag → swag-apifox → Apifox` 这条链路的中间，不绑定任何业务项目，也不改动 swag 本身的行为。

## 解决什么问题

swag v2 生成的 OpenAPI 3.1 里，有两处信息 Apifox 用不上：

| swag 的输出 | 问题 | 本工具生成 |
| --- | --- | --- |
| `x-nullable: true` | Apifox 更认 `type: 'null'`，`any` 字段被当成普通类型 | 保留描述性键 + `type: 'null'` |
| `enum` + `x-enum-varnames` + `x-enum-comments` | 枚举项的中文说明在 Apifox 里不显示 | `x-apifox-enum`（`value` / `name` / `description`） |

此外还能从字段注释、`@Param ... Enums(...)`、中文说明（`1-禁用，2-启用`、`+ 日常收支：1`）、query 参数父级 description、`$ref` 指向的 schema 等位置补全枚举标签。

## 特性

- Go 编写，可 `go install` 安装；[cobra](https://github.com/spf13/cobra) 管理命令与参数
- 两个子命令：`build` 一键调用 swag 并后处理，`process` 只后处理已有文档（支持 stdin / stdout）
- **纯文本行级编辑，不反序列化 YAML**：swag 的缩进、键顺序、引号风格原样保留
- 幂等：对已处理的文档重复运行，结果逐字节不变
- **实时透传 swag 输出并着色**（进度青 / 提示黄 / 错误红），解析大项目时不会像卡死；`--color` 可控制，`NO_COLOR` / 重定向自动关色
- 转换规则拆成可插拔的 transformer，后续增加更多 `x-apifox-*` 字段只需加一个实现

---

## 运行环境要求

> **`build` 依赖外部的 `swag` 可执行文件，必须事先安装并保证能被调用。本工具不内置、也不会自动下载 swag。**

| 依赖 | 版本 / 要求 | 何时需要 |
| --- | --- | --- |
| `swag`（[swaggo/swag v2](https://github.com/swaggo/swag/tree/v2)） | v2.0.0+，必须支持 `--v3.1` 与 `--ot yaml` | 执行 `build` 且未加 `--skip-swag` 时 |
| Go | 1.25+ | 仅从源码 `go install ./...` / `go build` 时；使用已编译二进制不需要 |
| 目标项目的源码与依赖 | 能被 swag 正常解析（`main.go` / `.swaggo`） | `build` 会在 `--root` 目录内执行 swag |

安装 swag：

```sh
go install github.com/swaggo/swag/v2/cmd/swag@latest
swag --version        # 应输出 v2.0.0 一类的 v2 版本
```

注意：

- **必须是 v2**。v1 的路径是 `github.com/swaggo/swag/cmd/swag`（没有 `/v2/`），产出 Swagger 2.0 且不支持 `--v3.1`，本工具无法处理其产物。
- 把 `$(go env GOPATH)/bin`（Windows 为 `%GOPATH%\bin`）加入 `PATH`；也可以用 `--swag /绝对路径/swag` 显式指定可执行文件，CI 里推荐这么做。
- swag 找不到或执行失败时，`build` 会以非零码退出（例如 `swag init: exec: "swag": executable file not found in %PATH%`）。
- **不需要 swag 的两种用法**：`process`（只吃已有 YAML 或 stdin）、`build --skip-swag`（复用项目里已有的 `docs/swagger.yaml` / `docs/openapi.yaml`）。纯后处理场景可以完全不装 swag。
- swag 的日志会实时透传并着色（见「输出与颜色」）；它会往 stderr 写常量求值一类的警告但退出码为 0，工具只按退出码判定成败。

## 安装

```sh
# 公网仓库（推送并打 tag 之后）
go install github.com/Robert-Du0001/swag-apifox@latest

# 本地仓库
git clone https://github.com/Robert-Du0001/swag-apifox.git
cd swag-apifox
go install ./...          # 装到 $(go env GOPATH)/bin/swag-apifox[.exe]
```

也可以只构建二进制：

```sh
go build -o swag-apifox .
```

> `go install <module>@latest` 要求仓库已推送到 GitHub 并打了语义化 tag（如 `v0.1.0`）；未发版前请用 `go install ./...`。

## 快速开始

先确认 `swag --version` 可用（见「运行环境要求」），然后在目标项目根目录执行：

```sh
# 一键：跑 swag init → 后处理 → 写 docs/openapi.yaml
swag-apifox build

# 或显式指定目标项目根
swag-apifox build --root <目标项目根>

# 需要给 swag 加参数时，用 -- 原样穿透（见「swag 参数穿透」）
swag-apifox build -- "--parseDependency --parseDepth 1"

# 只做后处理（不调用 swag），适合 CI / 管道
swag-apifox process -i docs/swagger.yaml -o docs/openapi.yaml
cat docs/swagger.yaml | swag-apifox process -i - > docs/openapi.yaml
```

`build` 会把 swag 的原始输出写进系统临时目录，`docs/swagger.yaml` 不会成为交付物；成功后默认删除它（`--keep-swagger` 可保留）。swag 在**项目根**执行，这样 `.swaggo` 里的类型映射与 `main.go` 的通用注解才能生效。

## 命令与参数

| 命令        | 说明                    |
| --------- | --------------------- |
| `build`   | 一键：调用 swag → 后处理 → 输出 |
| `process` | 只后处理一个已有文档（文件或 stdin） |

| 参数                      | 默认                                              | 说明                                                                  |
| ----------------------- | ----------------------------------------------- | ------------------------------------------------------------------- |
| `--root`                | 从 cwd 向上找 `main.go` / `.swaggo`                 | 目标项目根；swag 在此目录执行                                                   |
| `-o, --out`             | build: `docs/openapi.yaml`；process: `-`(stdout) | 相对 `--root`；绝对路径原样使用；`-` 表示 stdout                                  |
| `-i, --input`           | `-`                                             | 仅 `process`：输入文件或 `-`(stdin)                                        |
| `--enum-label-field`    | `description`                                   | 中文标签写进 `x-apifox-enum` 的 `description` / `name` / `both`            |
| `--enable`              | `enum,nullable`                                 | 启用的 transformer（可重复）                                                |
| `--swag`                | `swag`                                          | 仅 `build`：swag 可执行文件（PATH 名或绝对路径）                                  |
| `-- <swag 参数>`         | 无                                               | 仅 `build`：`--` 之后的全部内容原样交给 `swag init`；不传即只跑基础命令                     |
| `--skip-swag`           | 关                                               | 仅 `build`：不跑 swag，按 `docs/swagger.yaml` → `docs/openapi.yaml` 取已有文档 |
| `--keep-swagger`        | 关                                               | 仅 `build`：保留 `docs/swagger.yaml`                                    |
| `--string-enums`        | 关                                               | 额外解析字符串枚举值（如 `ASC-升序`）；默认关闭以保持字节兼容                                |
| `--strict`              | 关                                               | 校验 `enum` 与 `x-apifox-enum` 条目数一致、无空标签，不一致则非零退出                     |
| `--quiet`               | 关                                               | 静默 swag 输出与统计块                                                       |
| `--verbose`             | 关                                               | 额外打印实际执行的完整 swag 命令行                                                 |
| `--color`               | `auto`                                          | 着色模式：`auto` / `always` / `never`（见「输出与颜色」）                          |
| `--version`             | —                                               | 版本号                                                                 |

### 输出与颜色

`build` 会**逐行实时**把 swag 的 stdout / stderr 转发到自己的 stderr，不缓冲、不等命令结束，所以解析大项目时能看到进度，不会像卡住。默认按内容着色：

| 颜色 | 匹配的内容 |
| --- | --- |
| 青色 | `Generating ...`、`Generate ...`、`create ...` 等进度行 |
| 黄色 | `Override detected`、`deprecated`、`warn`、`found schema with no Type` 等提示 |
| 红色 | `Error`、`cannot find`、`failed`、`panic:` 等错误行 |
| 绿色 | 统计块末尾的 `done.` |

统计信息（source、转换计数、输出行数）同样写到 **stderr**；stdout 永远只有 YAML，所以 `process -i - > out.yaml` 是干净的。

`--color` 控制着色：

| 值 | 行为 |
| --- | --- |
| `auto`（默认） | 交互式终端启用；`NO_COLOR`、`TERM=dumb`、stderr 重定向到文件时自动关闭 |
| `always` | 强制启用，即使输出被重定向 |
| `never` | 完全关闭 |

`CLICOLOR_FORCE=1` 可在 `auto` 下强制启用（CI 日志想保留颜色时用）。Windows 传统控制台会自动开启 VT 模式；Windows Terminal、Git Bash 等自带 ANSI 支持的终端无需额外设置。

`--quiet` 会连 swag 输出和统计块一起静默，`--verbose` 则额外打印实际执行的完整 swag 命令行。

### swag 参数穿透

`build` 的基础命令固定为 `swag init --v3.1 --ot yaml`（后处理只认 OpenAPI 3.1 YAML），**默认不带任何额外参数**。要加参数，用标准的 `--` 分隔符把 swag 自己的选项写在后面：

```sh
# 默认：等价于 swag init --v3.1 --ot yaml -o <工具临时目录>
swag-apifox build

# 逐个参数写（推荐，最贴近原生 swag 命令）
swag-apifox build -- --parseDependency --parseDepth 1

# 也可以把整串用引号包起来写成一个参数
swag-apifox build -- "--parseDependency --parseDepth 1"

# 含空格的值交给 shell 分组即可
swag-apifox build -- --parseDependency --exclude "./vendor dir" --parseDepth 1
swag-apifox build -- "--exclude './vendor dir' --parseDepth 1"
```

- `--` 是 cobra/pflag 的标准分隔符：它之后的内容不再被本工具解析成 flag，而是原样作为位置参数收下，再逐个追加到 `swag init --v3.1 --ot yaml` 之后。所以 `--parseDependency` 这类本工具没定义的选项不会触发 `unknown flag`。
- 整串写法（`-- "--a --b 1"`）会按 shell 风格切分：单/双引号分组、反斜杠转义空格，**不做**变量展开或命令替换；引号没闭合会直接报错。逐项写法不切分，含空格的值保持原样。两种写法可以混用。
- `--` 必须放在 `build` 及其自身 flag 之后；`--` 之前出现位置参数会报错，避免漏写 `--` 时被静默忽略。`--skip-swag` 与 `--` 同时带参数也会报错，因为此时根本不跑 swag。
- `-o <临时目录>` 由工具**始终追加且不可覆盖**：swag 的原始输出必须落到工具读取的临时目录，否则拿不到输入。它不属于可配参数，你自己不要传 `-o`。完整拼装顺序是 `init --v3.1 --ot yaml` → `-- 之后的参数` → `-o <临时目录>`。
- `--verbose` 会把实际执行的完整命令打到 stderr，便于确认穿透结果；平时 swag 日志已默认打印，它只是额外多这一行。

## 做了哪些转换

1. **`x-nullable` → `type: 'null'`**：Go 结构体标签 `extensions:"x-nullable"` 会被 swag 输出成 `x-nullable: true`；Apifox 更认 `type: 'null'`。整段节点重建，只保留 `description / example / title / form` 四个描述性键。
2. **schema 级枚举 → `x-apifox-enum`**：swag 为具名常量类型生成 `enum` + `x-enum-varnames` + `x-enum-comments`，工具按 varnames 的位置把中文注释对齐成：
   ```yaml
   x-apifox-enum:
     - value: 1
       name: ''
       description: 禁用
   ```
3. **字段/参数级枚举**：从字段注释、`@Param ... Enums(...)`、内联中文说明（`1-禁用，2-启用`、`+ 日常收支：1`）、query 参数父级 description、`$ref` 指向的 schema，以及「唯一最佳匹配」推断中补全标签；必要时顺带生成 `enum:` 块。

   标签从哪个字符开始由正则决定，**到哪里结束则由 `internal/labels/scanLabelEnd` 逐字符扫描决定**（正则只能一个字符一个字符地看，表达不了「括号要配对」）：
   - 括号外的空白、`，`、`,`、`、`、`；`、`;`、`/`、`。` 结束标签；
   - `（`/`(` 与 `）`/`)` 按层数配对，**括号内的分隔符不截断标签**：`3 只读（0 = 不改；管理员永远是 1）` 的标签是整段，不会被 `；` 切掉；
   - 没有对应开头的 `）`/`)` 视为终点（它属于标签外面的文字）；扫描到文本结尾还留着未闭合的 `（` 时，**左括号及其后的内容整段丢弃**（`1 启用（默认` → `启用`）。

   也就是说括号里的补充说明只有「整段保留」和「整段丢弃」两种结果，不会出现 `读写（默认` 这种半个括号。
4. **幂等剥离**：每次运行先删掉上一轮生成的 `x-apifox-enum` 块（保留 `enum:`），再重新生成。

默认启用 `enum` 与 `nullable` 两个 transformer。Apifox 导出里还有 `x-apifox-name`、`x-apifox-ordering`、`x-apifox-mock`、`x-apifox-orders`、`x-apifox-refs` 等字段，可按下面的方式扩展。

## 扩展新的 Apifox 字段

`internal/transform/registry.go` 里定义了扩展点：

```go
type Transformer interface {
    Name() string
    Strip(doc *document.Document)                 // 删掉本规则生成的块，保证可重复运行
    Apply(ctx *Context) ([]document.Edit, error) // 基于未修改的行收集编辑
}
```

新增一个 `x-apifox-*` 字段只需三步：

1. 在 `internal/transform/` 新建文件实现接口，`Strip` 只删自己生成的行，`Apply` 只收集 `document.Edit`（`Order` 与已有规则错开）；
2. 在 `NewRegistry` 里注册，并在 `runner.Config.Validate` 的 `known` 表加入名字；
3. 用 `--enable <name>` 启用（默认不启用，避免影响既有产物的字节一致性）。

`document` 包只提供行模型、缩进、节点索引、标量格式化与「倒序应用编辑」，**不要**引入 YAML 解析库重新序列化整份文档——那会破坏键顺序和引号风格。

## 测试

```sh
go test ./...
```

测试全部使用内联的最小 YAML 样本，仓库里没有任何夹具文件，也不依赖真实项目数据：

- `internal/document`：BOM / CRLF / 尾部空行、节点索引、`$ref` 与「列表项与 key 同缩进」、标量格式化、块标量 description、编辑倒序应用。
- `internal/labels`：三种中文枚举说明写法、汉字闸门（日期/金额不误判）、全角标点、括号配对（整段保留 / 未闭合丢弃 / 嵌套 / 括号内分隔符）、字符串枚举开关、父级回看 8 行。
- `internal/transform`：三个 Step、label 三态、幂等、剥离规则。
- `internal/runner`：端到端流水线、`--enum-label-field`、`--strict` 的异常检测、`--` 参数规范化与 swag 命令拼装、输出行缓冲与着色分级、`--color` 模式与环境变量。

想验证升级后产物不变，可以自己留一份已生成的 YAML 当基准，不需要把大文件放进仓库（比对时要传与生成基准时**相同**的 `--` 参数，否则参数不同产物自然不同）：

```sh
swag-apifox build --root <目标项目根> -o /tmp/candidate.yaml --quiet -- <生成基准时用的参数>
diff -u <基准文件> /tmp/candidate.yaml
```

## 已知边界

- 输入是 **swag v2 在 `--v3.1 --ot yaml` 下的产物**；换 swag 版本或输出格式后，用上面的 `diff` 命令重新确认缩进假设。
- 默认只输出 YAML；JSON 输出暂不支持。
- 默认只识别数字型枚举值，`--string-enums` 打开后才解析字符串值（且会改变产物字节）。
- 默认只生成 `x-apifox-enum` / `type: 'null'` 两类扩展；其余 `x-apifox-*` 通过 transformer 扩展。

## 发版

- 确认 `go.mod` 的 module 与 GitHub 仓库地址一致（当前 `github.com/Robert-Du0001/swag-apifox`）。
- 打语义化 tag（如 `v0.1.0`）之后 `go install github.com/Robert-Du0001/swag-apifox@latest` 才可用。

## License

MIT
