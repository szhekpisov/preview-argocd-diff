# Smoke fixture

Minimal Helm chart + ArgoCD Application used to exercise the end-to-end
pipeline against a real Kind cluster.

Two branches, identical except for `charts/nginx-mock/values.yaml`:

| branch             | `image.tag` | `replicas` |
| ------------------ | ----------- | ---------- |
| `smoke-base-v1`    | 1.25        | 2          |
| `smoke-head-v1`    | 1.26        | 3          |

Running:

```sh
preview-argocd-diff run \
  --repo szhekpisov/preview-argocd-diff \
  --base-branch smoke-base-v1 \
  --head-branch smoke-head-v1 \
  --output-dir /tmp/padp-smoke \
  --log-level debug
```

Expected: Kind cluster `padp` comes up, ArgoCD installs, the
`nginx-mock` Application is rendered on both refs, and
`/tmp/padp-smoke/diff.md` contains a diff showing `image: nginx:1.25`
→ `image: nginx:1.26` and `replicas: "2"` → `replicas: "3"`.
