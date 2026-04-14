---
name: release
description: 执行项目发布流程 —— 更新 CHANGELOG、提交、打 tag、推送。顺序严格，避免 tag 先于 CHANGELOG。用法：/release vX.Y.Z
---

# Release Skill

按以下**严格顺序**执行。任何一步失败立即停止并报告，不要跳过。

## 前置检查

1. `git status` —— 必须在 `main` 分支且 working tree 干净（未提交改动会混进发布 commit）。如果有未提交改动，先问用户是否 commit 或 stash。
2. 解析参数中的版本号 `vX.Y.Z`。如果用户未提供，问一下想发什么版本（上一个 tag 可用 `git tag --sort=-v:refname | head -1` 查）。
3. 确认该 tag 不存在：`git rev-parse vX.Y.Z 2>/dev/null` 应失败。

## 步骤

### 1. 收集 commits
```
git log $(git describe --tags --abbrev=0)..HEAD --oneline
```
把这些 commit 分类（feat / fix / docs / chore / refactor）写入 CHANGELOG.md 顶部新章节。

### 2. 更新 CHANGELOG.md
格式参考既有章节（见文件头）。典型结构：
```md
## vX.Y.Z

### 新功能 / 改进 / 修复 / 内部
- <简短中文描述>
```
只写用户视角能感知的变化，内部重构放 `### 内部`。

### 3. Commit
```
git add CHANGELOG.md
git commit -m "chore: release vX.Y.Z"
```

### 4. 打 tag 并推送
```
git tag vX.Y.Z
git push origin main
git push origin vX.Y.Z
```
两次 push 分开执行。CI 会基于 tag 自动构建 release 产物。

## 注意事项

- **绝不先打 tag 再写 CHANGELOG**。tag 必须指向包含该版本 CHANGELOG 的 commit。
- 如果 CI 已基于错误 tag 跑了，默认选择只推 CHANGELOG 到 main，下次 release 补上；不要 force-push tag 除非用户明确要求。
- 本项目没有 `package.json` / `Cargo.toml` 之类的版本声明文件，只有 tag 和 CHANGELOG 需要更新。`build.sh` 里的 `VERSION=1.0.0` 是静态字符串，不用改。
