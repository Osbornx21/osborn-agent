SHELL := /bin/bash

PLAN := docs/superpowers/plans/2026-06-06-stackchan-xiaozhi-server.md
INVENTORY := STACKCHAN_XIAOZHI_TOOLING_INVENTORY.md
CONTROL_DOCS := docs/control/README.md docs/control/MAINLINE_STATUS.md docs/control/PROJECT_CONTROL.md docs/control/TASK_BOARD.md docs/control/VERIFICATION_GATES.md docs/control/PROVIDER_INTEGRATION_GATES.md docs/control/SUBAGENT_POLICY.md docs/control/SECRETS_POLICY.md docs/control/CHANGE_PROTOCOL.md docs/control/ARCHITECTURE_DECISIONS.md

.PHONY: control-check control-status control-plan-scan control-secret-scan control-files

control-check: control-files control-plan-scan control-secret-scan control-status

control-files:
	@test -f "$(INVENTORY)"
	@test -f "$(PLAN)"
	@for file in $(CONTROL_DOCS); do test -f "$$file"; done

control-plan-scan:
	@! rg -n "TBD|TODO|implement later|fill in details|add appropriate|write tests for the above|Similar to Task" "$(PLAN)" docs/control README.md

control-secret-scan:
	@! rg -n "(sk-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|xox[baprs]-[0-9A-Za-z-]{20,}|-----BEGIN (RSA |OPENSSH |EC )?PRIVATE KEY-----)" . --glob '!Makefile' --glob '!docs/control/SECRETS_POLICY.md'

control-status:
	@git status --short
