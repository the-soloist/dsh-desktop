---
name: desktop-release
description: 在 dsh-desktop 中按 SemVer 更新 VERSION、分组 commit，推送分支并确认 CI 成功后创建和推送版本 tag。用于项目发布、semver patch/minor/major tag、创建标签并提交推送等请求；普通 commit/push 不触发版本升级。
---

# DSH Desktop 版本发布

执行顺序：确定版本 → 更新并验证 VERSION → commit → push 分支 → 确认该提交 CI 成功 → 创建并 push tag → 核验远端。tag 必须指向包含版本更新和本次发布内容、且已通过 CI 的同一提交。

## 范围与授权

明确调用 `$desktop-release patch`（或 minor/major）表示执行完整发布流程；用户指定“只更新版本”“只打本地 tag”“不要 push”时，执行到对应边界。

检查已有改动与暂存内容，只提交用户授权的发布范围。用户已明确要求 commit/tag/push 时，该范围内的版本提交无需重复确认；仅要求 tag 而仍需提交版本文件时，先说明并取得提交授权。无关改动保留原样，无法隔离时暂停，不使用 `git add .`、stash 或重置来腾空工作区。

编写或审查本 skill 本身不构成发布授权。完整发布包含推送 tag 前等待 CI 成功；本地构建、安装应用及推送 tag 后持续监控 Release 仅在任务包含这些动作时执行。只创建本地 tag 时保留本地验证，远端 tag 发布仍须通过 CI 检查。

## 项目约定

- [VERSION](../../../VERSION) 是桌面版本的唯一来源；[Go 元数据](../../../version.go)和[构建配置](../../../scripts/build/config.ts)读取它。使用严格的 `x.y.z`，每段不超过 65535。DSH npm 依赖的 alpha/rc 不是桌面发布版本。
- 版本 tag 为 `v${VERSION}`，使用附注标签；[Release workflow](../../../.github/workflows/release.yml)要求 tag 名与 VERSION 一致。推送 tag 将触发跨平台构建和 GitHub Release 发布。
- 常规发布从 `main` 推送至 `origin/main`。先核实实际分支、远端和项目指令；目标不符时说明情况，不擅自切换分支或覆盖远端。
- commit 使用 `type(scope): 中文标题`，空行后用 `- 修改内容` 描述实际变更。独立变更分组提交，明确暂存文件或 hunks；仅剩版本更新时可单独使用 `chore(release)`。
- 包和中间产物留在忽略的 `dist/`，发布只推送目标分支与目标 tag。

## 1. 检查本地与远端

读取适用的 AGENTS.md、VERSION、相关差异及当前 workflow，确认工作区和暂存区：

```sh
git status --short --branch
git diff
git diff --cached
git log -8 --oneline
git tag --merged HEAD --list 'v*' --sort=-version:refname
git remote -v
```

获得联网权限后查询远端 main 和版本标签，并获取当前 main。基线只选当前分支可达且符合 `vX.Y.Z` 的桌面 release tag，不能把其他引用或预发布 tag 当正式基线。

```sh
git ls-remote origin refs/heads/main 'refs/tags/v*'
git fetch origin main --tags
git merge-base --is-ancestor origin/main HEAD
```

最后一条失败时停止发布并解释远端领先或分叉，按用户方向处理后重新验证，不自动 rebase、merge 或强推。网络或认证失败同样暂停，不能把失败当作远端不存在。

对恢复中的发布，先由当前 VERSION、已有 tag、远端状态和对话确定目标，沿用该目标。不要在每次重试时再次递增；目标存在歧义时先询问。

## 2. 确定并更新版本

尊重用户指定的版本或递增级别；未指定时检查上次发布以来的提交、未提交差异及实际行为：

- patch：兼容性修复、文档或路径修正。
- minor：向下兼容的新能力，patch 归零。
- major：不兼容的公共接口或行为变化，minor 和 patch 归零。

先说明选择依据和旧、新版本。已有尚未发布的正确版本更新时复用，不重复递增。没有待发布变更且用户没有要求空版本发布时，说明现状。

可用时读取当前环境的 `semver` skill，使用其 bump.py，限定 `--file VERSION --root <仓库根目录>`，先 `--dry-run` 再执行。按 skill 的实际位置定位脚本，不写死某台机器的 home 路径。不可用时预览计算结果，再仅修改 VERSION，并用项目解析器核验。不升级 Go、Wails、Bun 或 npm 依赖版本。

实施前确认目标 tag 在本地及远端不存在。既有 tag 若不属于这次恢复流程，停止并报告冲突，不移动、删除或重建已发布标签。

## 3. 验证并提交

至少执行版本解析测试和空白检查：

```sh
go test .
git diff --check
```

结合待发布范围选择现有检查；Go 行为变化按当前 shell 设置 `CGO_ENABLED=0`，运行 `go test -tags server ./...` 和 `go vet -tags server ./...`；前端变化运行 `bun test tests`；构建逻辑变化按实际 CI 补充构建验证。相同输入已通过的证据可复用，VERSION 改变后重验嵌入版本。

Go 缓存、临时文件和构建验证输出需要自定义位置时放到 `dist/intermediate/`。缺少权限、工具链或检查失败时，报告阻碍并保留修改，不跳过 hook 或带失败推送。

逐组查看工作区与暂存差异，明确 `git add` 的文件或 hunks，然后检查暂存内容并 commit。VERSION 与所属修复同组，修复已提交时单独提交版本更新。完成后确认没有遗漏的发布内容或无关暂存，记录最终提交 OID。

## 4. 推送分支并确认 CI

确认 HEAD 中的 VERSION 与计划一致，记录其精确 OID。以下示例使用已核实的 main/origin 和 POSIX shell；其他 shell 使用等价语法：

```sh
set -e
release_version="$(tr -d '[:space:]' < VERSION)"
release_tag="v${release_version}"
release_commit="$(git rev-parse HEAD)"
test "$(git show HEAD:VERSION | tr -d '[:space:]')" = "$release_version"
git push origin main
git ls-remote origin refs/heads/main
gh run list --workflow ci.yml --branch main --event push --commit "$release_commit" \
  --limit 20 --json databaseId,attempt,headSha,event,headBranch,status,conclusion,url
```

先确认远端 main 指向 `release_commit`。使用当前 [CI workflow](../../../.github/workflows/ci.yml) 的文件名筛选运行；若流程已改名或触发条件改变，按真实配置调整，不用其他 workflow 的绿色结果替代。

选择该 SHA、分支、push 事件对应的最新运行及最新 attempt，将读取到的 run ID 记为 `ci_run`。排队或运行中时等待，不把命令退出码为零当作检查成功：

```sh
gh run watch "$ci_run" --exit-status --interval 10
gh run view "$ci_run" --json attempt,headSha,event,headBranch,status,conclusion,jobs,url
```

放行条件必须同时满足：

- 运行的 `headSha` 等于 `release_commit`，事件是目标分支的 push，且没有更新的运行或 attempt 尚待检查。
- workflow 的 `status` 为 `completed`，`conclusion` 为 `success`。
- 根据当前 CI 和 [原生构建矩阵](../../../.github/workflows/native-build.yml)核对所有必需 job 都完成且成功，包括质量检查、macOS Universal、Linux x86_64 和 Windows x86_64；必需 job 被跳过也不能放行。诊断上传等条件步骤的正常跳过不属于 job 失败。

推送后暂未出现运行时可等待事件可见，并持续提供进度；单次工具阻塞等待不超过 60 秒。持续查不到运行、gh/API 或认证不可用时报告阻碍，保持 tag 未推送；可改用 GitHub API 或浏览器核验，但证据标准不变。

失败、取消、超时、等待审批或未完成都不允许发布 tag。读取失败日志并报告，不自动重跑或制造空提交。修复已获授权时，修复后重新提交和推送，更新 `release_commit` 并等待新 SHA 的完整 CI；旧 SHA 的成功结果不能替代。

## 5. 创建并推送 tag

CI 成功后，再查询最新匹配运行及其 attempt，确认仍满足放行条件。重新核实本地 HEAD、远端 main 都是已验证的 `release_commit`，版本内容未变化；并发更新或新增提交时暂停，重新确定发布目标并检查相应 CI。

```sh
set -e
test "$(git rev-parse HEAD)" = "$release_commit"
test "$(git show HEAD:VERSION | tr -d '[:space:]')" = "$release_version"
git ls-remote origin refs/heads/main "refs/tags/${release_tag}" "refs/tags/${release_tag}^{}"
git tag -a "$release_tag" -m "Release $release_tag" "$release_commit"
test "$(git rev-parse "${release_tag}^{commit}")" = "$release_commit"
test "$(git show "${release_tag}:VERSION" | tr -d '[:space:]')" = "$release_version"
git push origin "refs/tags/${release_tag}"
```

检查 ls-remote 的结果后才执行 tag 操作。恢复流程已存在正确附注标签时直接复用，不再次创建；本地 tag 指向其他提交时先报告，不擅自移动。只推送这一个 tag，不强推、镜像推送或推送全部 tags。

分支与 tag 分两阶段推送，CI 是两者之间的发布检查点。失败时保留已推送的分支及本地状态，修复或补齐证据后从同一目标继续；结果不确定先查询远端，不盲目重推、回滚分支或再次递增版本。

## 6. 核验与交付

重新查询远端 main、目标 tag 和附注标签的 `^{}`，确认分支及标签指向记录的发布 OID，再查看本地工作区。普通推送不会因成功返回就证明 Release 构建已完成。

提供发布前通过的 CI 运行链接、SHA 和 attempt。推送 tag 后只读查询 Release workflow，报告实际链接及 pending/success/failure；用户未要求持续监控时不等待 Release 完成，也不自动重跑失败任务或改发布流程。

交付旧、新版本、修改的版本文件、各 commit、CI 证据、tag、远端核验和实际验证结果，并说明工作区剩余内容。CI 未满足条件时明确写“分支已推送，tag 尚未推送”。仅完成版本与推送时，不声称本地已安装应用也已更新。
