# OPS-43 owner acceptance

- [ ] Confirm Docker Hub tokens are restricted to the intended repositories and required publication operations. Credential scope cannot be inferred from workflow source; do not expose token values in evidence.
- [ ] Review the upstream Action pin record in `docs/workflow-security.md` before merging subsequent dependency updates.
- [ ] Run an explicitly authorized release/publication acceptance check after merge when Actions capacity is available. This branch was validated locally with synthetic fixtures only; no registry write, release, image publication, live settings change, credential rotation, or workflow retry was performed.
- [ ] Confirm any required protected environment approvals / repository Action allowlist through the owner. No live repository protections were changed by this code-only proposal.
- [ ] When combining container-validation PR #10 (head `099a247d41c41e6a88673c743d34599a8bfa808a`), retain the SHA policy by pinning its new QEMU step to `99012661954931238ded8c8b007157a8430204e1` (`v4.4.0`) and Buildx step to `f87e5991a6d7451dcb8d9637bfbc97413f497069` (`v4.4.1`). That head introduces mutable tags and is rejected by this policy; the pins were verified against the same upstream releases already recorded above.
