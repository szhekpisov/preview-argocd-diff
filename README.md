# preview-argocd-diff

A Go CLI that renders the diff of ArgoCD Applications / ApplicationSets affected
by a pull request and posts the result as a PR comment.

Inspired by [dag-andersen/argocd-diff-preview](https://github.com/dag-andersen/argocd-diff-preview),
with three differences:

- **Only-changed by default** — computes the set of affected Apps from
  `git diff base..head` and renders only those.
- **Prebuilt image** with Kind + ArgoCD + Helm baked in, so each run skips
  60–90s of cluster bootstrap.
- **Pluggable diff tool** — `--diff-tool` + `--diff-args` template with
  `{base}` / `{head}` placeholders.

Scoped to Helm charts for v1. Kustomize is a future add.

## Status

Pre-alpha. See [the implementation plan](docs/plan.md) (TODO) for the roadmap.

## Build

```
go build ./cmd/preview-argocd-diff
```

## License

MIT.
