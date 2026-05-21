# DEPRECATED — Helm chart

**Status**: not used in production.
**Date deprecated**: 2026-05-21.
**Reason**: project switched deploy target to **Docker Swarm on Contabo VPS**. See `infra/swarm/stack.{dev,prod}.yml` for active stacks.

## Why kept

- Skeleton (`templates/rollout.yaml`) retained as starting point if/when a K8s migration is required (e.g. moving to managed EKS/GKE at scale).
- Removing now loses the prior wiring effort; cheap to leave dormant.

## Do not extend

- Do NOT add new templates here.
- Do NOT wire Helm into CI (`.github/workflows/`).
- Do NOT reference this chart in deploy steps.

## To revive (future K8s migration)

1. Complete chart per Phase B in `documentation/10-implementation-plan.md` (deployment, service, ingress, configmap, sa, hpa, networkpolicy, servicemonitor).
2. Replace Swarm stacks in `.github/workflows/*` deploy steps with `helm upgrade --install`.
3. Bootstrap K8s cluster (likely managed: EKS/GKE/Linode LKE) via new Terraform module in `infra/terraform/` (currently empty).
4. Update CLAUDE.md "Deployment is microservices-first on **Docker Swarm**" → Kubernetes.
5. Update memory `project_deploy_target.md`.

## Related

- `infra/swarm/` — active Swarm stacks.
- `infra/ansible/` — VPS bootstrap playbook (to be added in Phase C).
- `documentation/10-implementation-plan.md` Phase B (Helm — deferred) + Phase C (self-hosted bootstrap — active).
