# State of Container Isolation — survey results

Scanned **16 scenarios** with `ironctl scan` dev on 2026-07-08T11:27:08Z.

Each row is one popular public image run with a specific configuration, graded 0-100 across seven containment dimensions (non-root user, dropped capabilities, seccomp, network isolation, read-only rootfs, no docker.sock, no host namespaces). Higher is safer. See [README.md](./README.md) for the exact method and [images.txt](./images.txt) for the pinned manifest.

| Scenario | Image | Score | Grade | Top failed dimensions |
|----------|-------|------:|:-----:|-----------------------|
| `naive-privileged` | `python:3.13-alpine` | 19/100 | **F** | Dropped capabilities, Non-root user (uid != 0), Seccomp profile |
| `naive-ci-docker-sock` | `node:22-alpine` | 33/100 | **D** | Dropped capabilities, Non-root user (uid != 0), No docker.sock exposure |
| `naive-host-ns` | `redis:7-alpine` | 34/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Network isolation / egress |
| `default-golang` | `golang:1.23-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-httpd` | `httpd:2.4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mongo` | `mongo:7` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-mysql` | `mysql:8.4` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-nginx` | `nginx:1.27-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-node` | `node:22-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-postgres` | `postgres:17-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-python` | `python:3.13-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-rabbitmq` | `rabbitmq:4-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-redis` | `redis:7-alpine` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-wordpress` | `wordpress:6-php8.3-apache` | 48/100 | **D** | Dropped capabilities, Non-root user (uid != 0), Read-only root filesystem |
| `default-memcached` | `memcached:1.6-alpine` | 63/100 | **C** | Dropped capabilities, Read-only root filesystem |
| `hardened-reference` | `nginx:1.27-alpine` | 100/100 | **A** | none |

**Grade distribution:** 1×A, 1×C, 13×D, 1×F.

Regenerate this file from a clean checkout with `examples/isolation-survey/survey.sh` (Docker required).
