---
name: desktop-release
description: 在 dsh-desktop 中按 SemVer 更新 VERSION、分组 commit，修复失败的发布前 CI 并持续提交验证，全部成功后创建和推送版本 tag。用于明确要求项目发布、semver patch/minor/major tag、创建版本标签并提交推送等请求。
---

# DSH Desktop 版本发布

执行顺序：确定版本 → 更新并验证 VERSION → commit → push 分支 → 确认该提交 CI 成功 → 创建并 push tag → 核验远端。CI 失败时循环执行“定位原因 → 修复 → 验证 → commit/push → 检查新提交 CI”，全部成功后再处理 tag。tag 必须指向包含版本更新和本次发布内容、且已通过 CI 的同一提交。

## 范围与授权

仅在用户明确要求桌面版本发布或版本标签流程时执行本流程。明确调用 `$desktop-release patch`（或 minor/major）表示执行完整发布流程；用户指定“只更新版本”“只打本地 tag”“不要 push”时，执行到对应边界。skill 的编写或审查任务仅处理 skill 文件。

检查已有改动与暂存内容，按文件或 hunks 选择用户授权的发布范围。用户已明确要求 commit/tag/push 时，按授权执行该范围内的版本提交；仅要求 tag 而仍需提交版本文件时，先说明并取得提交授权。其他改动保留原样，发布内容须可独立暂存，否则暂停并说明阻碍。

完整发布包含修复授权仓库内与本次发布有关的 CI 问题、提交推送修复，并等待 CI 成功。扩大到其他重构、凭据或权限修改、外部基础设施变更时，先取得单独授权。本地构建按验证需要执行，安装应用及推送 tag 后持续监控 Release 仅在任务包含这些动作时执行。只创建本地 tag 时完成本地验证；远端 tag 发布须通过 CI 检查。

## 项目约定

- [VERSION](../../../VERSION) 是桌面版本的唯一来源；[Go 元数据](../../../version.go)和[构建配置](../../../scripts/build/config.ts)读取它。使用严格的 `x.y.z`，每段取值为 0–65535。
- 版本 tag 为 `v${VERSION}`，使用附注标签；[Release workflow](../../../.github/workflows/release.yml)要求 tag 名与 VERSION 一致。推送 tag 将触发跨平台构建和 GitHub Release 发布。
- 常规发布从 `main` 推送至 `origin/main`。先核实实际分支、远端和项目指令；实际目标须与已授权的发布方案一致，有差异时先确认目标。
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

获得联网权限后查询远端 main 和版本标签，并获取当前 main。基线只选当前分支可达且符合 `vX.Y.Z` 的桌面 release tag。

```sh
git ls-remote origin refs/heads/main 'refs/tags/v*'
git fetch origin main --tags
git merge-base --is-ancestor origin/main HEAD
```

最后一条失败时暂停发布并解释远端领先或分叉，由用户确认整合方案，完成整合后重新验证。网络或认证失败时保留远端待核验状态，恢复查询成功后继续。

对修复或恢复中的发布，先由当前 VERSION、已有 tag、远端状态和对话确定目标；目标存在歧义时先询问。同一未发布版本的修复与恢复复用已确认版本号，每轮更新提交 SHA 和对应 CI 证据。

## 2. 确定并更新版本

尊重用户指定的版本或递增级别；未指定时检查上次发布以来的提交、未提交差异及实际行为：

- patch：兼容性修复、文档或路径修正。
- minor：向下兼容的新能力，patch 归零。
- major：不兼容的公共接口或行为变化，minor 和 patch 归零。

先说明选择依据和旧、新版本，复用已有且正确的待发布版本更新。待发布内容为空时说明现状，仅在用户明确要求空版本发布时继续。

可用时读取当前环境的 `semver` skill，通过该 skill 的实际目录定位 bump.py，限定 `--file VERSION --root <仓库根目录>`，先 `--dry-run` 再执行。需要手动更新时预览计算结果，再仅修改 VERSION，并用项目解析器核验。

新发布实施前须成功核验本地和远端均缺少目标 tag。修复或恢复中的发布按第 5 节处理本轮尚未推送的本地 tag；属于其他流程的既有 tag 按冲突处理。已发布标签固定指向原始 OID，修复已发布版本时使用新版本流程。

## 3. 验证并提交

至少执行版本解析测试和空白检查：

```sh
go test .
git diff --check
```

结合待发布范围选择现有检查；Go 行为变化按当前 shell 设置 `CGO_ENABLED=0`，运行 `go test -tags server ./...` 和 `go vet -tags server ./...`；前端变化运行 `bun test tests`；构建逻辑变化按实际 CI 补充构建验证。相同输入已通过的证据可复用，VERSION 改变后重验嵌入版本。

Go 缓存、临时文件和构建验证输出需要自定义位置时放到 `dist/intermediate/`。相关本地检查和 commit hook 成功后方可推送分支；缺少权限、工具链或检查失败时，报告阻碍并保留修改。

逐组查看工作区与暂存差异，明确 `git add` 的文件或 hunks，然后检查暂存内容并 commit。VERSION 与所属修复同组，修复已提交时单独提交版本更新。完成后确认授权的发布内容均已提交、其他改动保持原样，记录最终提交 OID。

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

先确认远端 main 指向 `release_commit`。使用当前 [CI workflow](../../../.github/workflows/ci.yml) 的实际文件名和触发条件筛选运行。

选择该 SHA、分支、push 事件对应的最新运行及最新 attempt，将读取到的 run ID 记为 `ci_run`。排队或运行中时等待，结束后读取运行详情并逐项核对放行条件：

```sh
gh run watch "$ci_run" --exit-status --interval 10
gh run view "$ci_run" --json attempt,headSha,event,headBranch,status,conclusion,jobs,url
```

放行条件必须同时满足：

- 运行的 `headSha` 等于 `release_commit`，事件是目标分支的 push，已核验的运行及 attempt 均为最新。
- workflow 的 `status` 为 `completed`，`conclusion` 为 `success`。
- 根据当前 CI 和 [原生构建矩阵](../../../.github/workflows/native-build.yml)核对所有必需 job 的状态均为 `completed` 且结论为 `success`，包括质量检查、macOS Universal、Linux x86_64 和 Windows x86_64。诊断上传等条件步骤按各自触发条件核验。

推送后等待对应运行可见，并持续提供进度；单次工具阻塞等待上限为 60 秒。持续缺少对应运行、可用查询工具或认证时，报告阻碍，tag 保持待推送；可通过 GitHub API 或浏览器按相同放行条件核验。

### CI 失败后的修复循环

排队、运行中或等待审批时继续等待；运行失败、取消或超时时执行以下循环，直至满足全部放行条件：

1. 读取失败 job 和日志，例如 `gh run view "$ci_run" --log-failed`，确认 SHA、attempt、平台及首个有效错误，定位所属源码、测试、依赖或构建配置。区分仓库缺陷、取消和外部基础设施问题。
2. 修复本次发布范围内的根因，运行相关本地验证并检查差异。保持既有测试、平台矩阵、断言和安全检查的验证契约有效，检查命令如实返回失败状态。
3. 将实际修复按项目格式单独 commit，再 push 分支。每轮提交包含实际变更；仅在新证据支持重试时重跑对应任务。
4. 更新 `release_commit` 为修复后的精确 SHA，重新查找该 SHA 对应的 CI 运行及最新 attempt，等待全部放行条件满足。仍失败就带着新证据回到步骤 1。

循环直到当前发布提交的 CI 全部成功，或用户要求暂停、明确缺少权限/凭据、外部故障阻断、下一步需要扩大授权范围。遇到后几种情况时保留提交和进度、如实报告 CI 状态及恢复所需条件，tag 保持待推送。

## 5. 创建并推送 tag

CI 成功后，再查询最新匹配运行及其 attempt，确认仍满足放行条件。重新核实本地 HEAD、远端 main 都是已验证的 `release_commit`，版本内容与计划一致；并发更新或新增提交时暂停，重新确定发布目标并检查相应 CI。

先成功查询远端目标 tag，再按实际状态处理：

- 本地和远端目标标签均缺失：在通过 CI 的 `release_commit` 上创建附注标签。
- 本地标签已正确指向该提交：复用并核验版本内容。
- 本轮发布的本地标签仍指向修复前提交，且成功查询远端确认缺少该 tag、当前提交 CI 已通过：记录旧、新 OID，使用 `git tag -d "$release_tag"` 删除这一个本地标签，再在通过 CI 的新提交上重建同名附注标签。操作范围仅限该本地标签。
- 本地标签属于其他流程，或远端同名标签指向不同提交：报告冲突并暂停。

查询成功是判断远端标签状态的前提，查询失败时暂停并保留待核验状态。远端已存在且正确指向目标提交时，视为标签推送已完成，转入远端与 Release 状态核验。

```sh
set -e
test "$(git rev-parse HEAD)" = "$release_commit"
test "$(git show HEAD:VERSION | tr -d '[:space:]')" = "$release_version"
git ls-remote origin refs/heads/main "refs/tags/${release_tag}" "refs/tags/${release_tag}^{}"
if ! git show-ref --verify --quiet "refs/tags/${release_tag}"; then
  git tag -a "$release_tag" -m "Release $release_tag" "$release_commit"
fi
test "$(git cat-file -t "$release_tag")" = tag
test "$(git rev-parse "${release_tag}^{commit}")" = "$release_commit"
test "$(git show "${release_tag}:VERSION" | tr -d '[:space:]')" = "$release_version"
git push origin "refs/tags/${release_tag}"
```

检查 ls-remote 的结果并完成上述标签状态处理后才执行推送。推送范围限定为 `refs/tags/${release_tag}`。

分支与 tag 分两阶段推送，CI 是两者之间的发布检查点。失败时保留已推送的分支及本地状态，修复或补齐证据后从同一目标继续；结果待确认时先查询远端，再确定恢复步骤。

## 6. 核验与交付

重新查询远端 main、目标 tag 和附注标签的 `^{}`，确认分支及标签指向记录的发布 OID，再查看本地工作区。Release 构建状态以对应 workflow 的实际结果为准。

提供发布前通过的 CI 运行链接、SHA 和 attempt。推送 tag 后只读查询 Release workflow，报告实际链接及 pending/success/failure。持续监控、重跑 Release 或调整发布流程，分别按用户对这些动作的授权执行。

交付旧、新版本、修改的版本文件、各 commit、CI 修复过程与最终成功证据、tag、远端核验和实际验证结果，并说明工作区剩余内容。CI 仍待满足放行条件时明确写“分支已推送，tag 尚未推送”。安装结果仅在实际执行安装并验证后报告。
