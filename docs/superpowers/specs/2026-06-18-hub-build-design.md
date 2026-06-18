# hub-build 特性设计：提交 Dockerfile 自动构建并发布镜像

- 日期：2026-06-18
- 状态：设计已确认，待评审
- 关联：复用并参考现有 `hub-mirror` 特性（`.github/workflows/hub-mirror.yml`、`main.go`、`pkg/cli.go`）

## 1. 目标与非目标

### 目标
- 用户在 Fork 仓库里提交一个 Issue（带 `hub-build` 标签），内含一段完整 Dockerfile，工作流自动 `build → tag → push` 到用户配置的镜像仓库，并把拉取命令以评论形式返回到 Issue。
- 复用 hub-mirror 既有基础设施：相同 Secrets、相同 `success`/`failure` 标签机制、相同 close-issues 收纳。
- 最大化复用 `pkg/cli.go`（registry 认证、流式错误解析）。

### 非目标
- 不支持构建上下文文件（`COPY` 本地文件无效）。Dockerfile 须自包含（`FROM`、`COPY --from=` 多阶段、`ADD <url>`、`RUN wget/curl` 等可用）。
- 不做多架构（buildx/QEMU）单次多架构 manifest；仅单架构构建。
- 不做批量（一个 Issue 仅一个 Dockerfile）。
- 不改动现有 hub-mirror 的 `main.go`/`hub-mirror.yml`/`output.tmpl`（零风险隔离）。

## 2. 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 构建上下文 | 纯 Dockerfile，空上下文 tar | 最简，贴合"提交一段 Dockerfile"原意 |
| 镜像命名 | 用户显式指定 `image`+`tag`，程序不改写 | build 场景用户全权控制命名，最少意外 |
| 架构 | 单架构，用户可选 `platform`（默认 linux/amd64） | 与 hub-mirror `platform` 语义一致 |
| 数量 | 一 Issue 一 Dockerfile | 简单可控 |
| 模板格式 | GitHub Issue Form（`.yml`） | textarea 原样粘贴、零转义、客户端必填校验、编辑时语法高亮 |

### 命名路由（与 hub-mirror 的有意差异）
hub-mirror 的 `Source2Target` 会把 `/`→`.` 拍平、并注入 platform 后缀，因为它复制的是他人多架构镜像。**hub-build 不改写名字**：用户填 `image`+`tag` 什么，就推 `repository/image:tag`；`platform` 只影响构建、不进镜像名。

## 3. 总体架构

```
提交 Issue(Issue Form) → hub-build.yml 触发 → go run ./cmd/hub-build
   → 解析表单 → NewCli(复用认证) → BuildImage(SDK ImageBuild)
   → PushImage(复用) → 渲染 output.md → 评论 + 打 success/failure 标签
```

复用 hub-mirror 全部外部依赖：`DOCKER_USERNAME` / `DOCKER_TOKEN` / `DOCKER_REPOSITORY` 三个 Secret；`success`/`failure` 标签；`actions/stale` 的 close-issues 自动收纳。新增一个 `hub-build` 标签做路由。

## 4. Issue 模板：`.github/ISSUE_TEMPLATE/hub-build.yml`

GitHub Issue Form。字段 `label` 用稳定 ASCII 串，便于 Go 按 `### <Label>` 解析；中文引导放 `description`。

```yaml
name: hub-build
description: 提交 Dockerfile，自动构建并发布镜像到你的镜像仓库
title: "[hub-build] 请求执行任务"
labels: ["hub-build"]
body:
  - type: input
    id: platform
    attributes:
      label: Platform
      description: 目标架构，可留空（默认 linux/amd64）。如 linux/arm64、arm64
      placeholder: linux/amd64
    validations:
      required: false
  - type: input
    id: image
    attributes:
      label: Image
      description: 镜像名（不含仓库前缀，程序自动拼接 repository/image:tag）
      placeholder: my-app
    validations:
      required: true
  - type: input
    id: tag
    attributes:
      label: Tag
      description: 镜像 tag
      placeholder: v1.0
    validations:
      required: true
  - type: textarea
    id: dockerfile
    attributes:
      label: Dockerfile
      description: 直接粘贴完整 Dockerfile，无需转义。无构建上下文：COPY 本地文件无效，COPY --from= 多阶段 / ADD <url> / RUN wget 可用
      render: dockerfile
    validations:
      required: true
```

提交后 `github.event.issue.body` 形如（Dockerfile 逐字保留，包在 ` ```dockerfile ` 围栏内）：

```
### Platform

linux/amd64

### Image

my-app

### Tag

v1.0

### Dockerfile

```dockerfile
FROM golang:1.21
WORKDIR /app
```
```

## 5. Go 程序

### 5.1 文件布局

```
cmd/hub-build/main.go   # 新入口：flag 解析 → 解析表单 → 校验 → build → push → 渲染
pkg/cli.go              # 改：抽 decodeStream；新增 BuildTarget / BuildImage
pkg/issueform.go        # 新：parseIssueForm + extractFenced
pkg/issueform_test.go   # 新：表单解析单测
pkg/build_test.go       # 新：BuildTarget 单测
output-build.tmpl       # 新：构建结果模板（仓库根，与 output.tmpl 并列）
```

不动：`main.go`、`pkg/cli_test.go`、`output.tmpl`、`hub-mirror.*`。

### 5.2 `pkg/issueform.go`（Issue Form body 解析）

```go
// parseIssueForm 把 GitHub Issue Form body 按 "### <Label>" 分段。
func parseIssueForm(body string) map[string]string

// extractFenced 去掉 ```dockerfile / ``` 围栏，返回围栏内原文。
func extractFenced(s string) string
```

解析逻辑：逐行扫描，遇 `### ` 开头则切节，节内容为到下一 `### ` 之间的文本（trim）；`extractFenced` 去掉首尾 ```` ``` ```` 行。表头键为 `Platform`/`Image`/`Tag`/`Dockerfile`（与模板 `label` 一致）。

### 5.3 `pkg/cli.go` 改动

**(a) 抽公共流式读取**（现 `PullImage` cli.go:144-163 与 `PushImage` cli.go:182-201 重复）：

```go
type streamMessage struct {
    Error        string `json:"error,omitempty"`
    ErrorMessage string `json:"errorMessage,omitempty"`
    ErrorDetail  *struct {
        Message string `json:"message,omitempty"`
    } `json:"errorDetail,omitempty"`
}

// decodeStream 逐行读取 daemon 输出写日志，并捕获错误。
// 兼容 pull/push 的 {"error"} 与 build 的 {"errorMessage"}/{"errorDetail":{"message"}}。
func (c *Cli) decodeStream(rd io.Reader) error
```

`PullImage`/`PushImage` 改为调用 `c.decodeStream(...)`，行为不变。

**(b) 构建命名**（与 `Source2Target` 并列，不改写）：

```go
func (c *Cli) BuildTarget(image, tag string) (string, error) {
    if image == "" || tag == "" {
        return "", errors.New("image or tag cannot be empty")
    }
    if c.repository == "" {
        return "docker.io/" + c.username + "/" + image + ":" + tag, nil
    }
    return c.repository + "/" + image + ":" + tag, nil
}
```

**(c) `BuildImage`**：构造仅含 Dockerfile 的 tar 上下文 → `ImageBuild`（`Tags: []string{target}` 直接打 tag，免去单独 `ImageTag`）→ `decodeStream` 捕获构建错误：

```go
func (c *Cli) BuildImage(ctx context.Context, dockerfile, platform, target string) error {
    // 1. bytes.Buffer + archive/tar 写入单文件 "Dockerfile"
    // 2. resp, err := c.cli.ImageBuild(ctx, &buf, types.ImageBuildOptions{
    //        Dockerfile: "Dockerfile", Platform: platform,
    //        Tags: []string{target}, Remove: true,
    //    })
    // 3. defer resp.Body.Close(); return c.decodeStream(resp.Body)
}
```

推送直接复用现有 `PushImage(ctx, target, platform)`（已带 `RegistryAuth: c.auth`）。

### 5.4 `cmd/hub-build/main.go` 主流程

对标 `main.go` 节奏：

```go
var (
    content    = pflag.String("content", "", "Issue Form body（为空时回退读 env ISSUE_BODY）")
    repository = pflag.String("repository", "", "")
    username   = pflag.String("username", "", "")
    password   = pflag.String("password", "", "")
    outputPath = pflag.String("outputPath", "output.md", "")
)
// 0. if *content == "" { *content = os.Getenv("ISSUE_BODY") }
//    （工作流经 env 传 body，绕开 shell 引号问题，见 §7）
// 1. sections := parseIssueForm(*content)
//    platform := sections["Platform"]; if "" → "linux/amd64"
//    image := sections["Image"]; tag := sections["Tag"]
//    dockerfile := extractFenced(sections["Dockerfile"])
// 2. 校验 image/tag/dockerfile 非空 → 否则 panic
// 3. cli, err := pkg.NewCli(ctx, repository, username, password, os.Stdout)  // 复用：登录+auth
// 4. target, err := cli.BuildTarget(image, tag)
// 5. cli.BuildImage(ctx, dockerfile, platform, target)
// 6. cli.PushImage(ctx, target, platform)
// 7. tmpl := template.Must(template.ParseFiles("output-build.tmpl"))
//    tmpl.Execute(outputFile, map[string]string{"Target": target, "Platform": platform})
// 任一步失败 → panic（非零退出 → 工作流 failure 分支）
```

无并发、无 `maxContent`（单 Dockerfile，更简单）。

## 6. 输出模板 `output-build.tmpl`

单镜像版，仿 `output.tmpl`：

````markdown
### 构建成功 ✅

镜像地址：`{{ .Target }}`
平台：`{{ .Platform }}`

### docker 版本

```shell
docker pull {{ .Target }}
```

### containerd 版本（以 k8s.io namespaces 为例）

```shell
ctr -n k8s.io image pull {{ .Target }}
```
````

## 7. 工作流 `.github/workflows/hub-build.yml`

逐行仿 `hub-mirror.yml`，仅改 name、label 过滤、run 入口：

```yaml
name: hub-build
on:
  issues:
    types: [opened, edited]
jobs:
  build_image:
    runs-on: ubuntu-latest
    if: contains(github.event.issue.labels.*.name, 'hub-build')
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v4
        with: { go-version: '1.20' }
      - name: Run code
        env:
          # 经 env 传 body：Dockerfile 常含单引号/反引号，inline --content='...' 会被破坏；
          # env 变量不经 shell 解析，安全。Go 端 --content 为空时回退读此 env（见 §5.4）。
          ISSUE_BODY: ${{ github.event.issue.body }}
        run: >-
          go run ./cmd/hub-build
          --username=${{ secrets.DOCKER_USERNAME }}
          --password=${{ secrets.DOCKER_TOKEN }}
          --repository=${{ secrets.DOCKER_REPOSITORY }}
          --outputPath=output.md
      # Add comment / Success issues / Failure issues 三步照搬 hub-mirror.yml：
      # - output.md 存在则评论；success() 打 success 标签；failure() 打 failure 标签 + 评论 Actions 日志链接
```

## 8. 错误处理与边界

| 场景 | 处理 |
|------|------|
| 必填项缺失（image/tag/dockerfile 空） | Issue Form 客户端已拦截；兜底 Go 校验 → panic |
| 表单解析异常 | panic（非零退出 → failure 标签） |
| Build 失败（COPY 本地文件/语法错/基础镜像拉取失败） | `decodeStream` 捕获 daemon 错 → panic → failure 标签 + 日志链接 |
| Push 失败（认证/配额） | 复用 `PushImage` 报错 → panic |
| platform 留空 | 默认 `linux/amd64` |

校验仅做基本非空/去空白检查，不过度校验 tag 合法性（交 daemon 报错，与 hub-mirror 风格一致）。

## 9. 测试

- `pkg/issueform_test.go`：覆盖典型 Issue Form body（含 platform 留空、多行 Dockerfile、多阶段）→ 断言 `parseIssueForm` 各字段与 `extractFenced` 围栏剥离正确。
- `pkg/build_test.go`：`BuildTarget` 在 `repository` 空/非空下的拼接结果（仿 `cli_test.go` 的 `stretchr/assert` 风格）。
- `BuildImage`/`PushImage` 涉及 daemon 与 registry，不做单测，靠工作流端到端验证。

## 10. 文件清单

| 操作 | 文件 |
|------|------|
| 新增 | `.github/ISSUE_TEMPLATE/hub-build.yml` |
| 新增 | `.github/workflows/hub-build.yml` |
| 新增 | `cmd/hub-build/main.go` |
| 新增 | `pkg/issueform.go` |
| 新增 | `pkg/issueform_test.go` |
| 新增 | `pkg/build_test.go` |
| 新增 | `output-build.tmpl` |
| 修改 | `pkg/cli.go`（抽 `decodeStream`；新增 `BuildTarget`/`BuildImage`） |
| 修改 | `README.md`（追加 hub-build 使用说明） |

## 11. 用户使用流程（README 新增章节）

1. Fork 项目，配置 3 个 Secret（同 hub-mirror）。
2. 确保 `Issues`、Actions 写权限已开启，`success`/`failure`/`hub-build` 标签已建。
3. 启用 `hub-build` workflow。
4. New issue 选 `hub-build` 表单：填 Image、Tag，把 Dockerfile 整段粘进 Dockerfile 框（无需转义）。
5. 提交后等 Actions 跑完，Issue 评论里给出 `docker pull` / `ctr pull` 命令，并打 `success`/`failure` 标签。
