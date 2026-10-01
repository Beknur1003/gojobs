PLIST_NAME := gojobs.daily
PLIST_DST  := $(HOME)/Library/LaunchAgents/$(PLIST_NAME).plist
REPO       := $(CURDIR)

.PHONY: help run build serve test lint fmt vet check daily install-daily uninstall-daily

help:
	@echo "run              - collect vacancies, update data/jobs.json, rebuild docs/"
	@echo "build            - rebuild docs/ from data/jobs.json without fetching"
	@echo "serve            - preview the site on http://localhost:8090/gojobs/"
	@echo "check            - fmt + vet + lint + tests with the race detector"
	@echo "daily            - what the daily job does: run, commit, push"
	@echo "install-daily    - schedule 'daily' every day at 09:00 with launchd"
	@echo "uninstall-daily  - remove the schedule"

run:
	go run ./cmd/gojobs run

build:
	go run ./cmd/gojobs build

serve:
	go run ./cmd/gojobs serve -addr localhost:8090

test:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

check: fmt vet lint test

daily:
	bash deploy/daily.sh && tail -n 5 logs/daily.log

# macOS blocks background jobs from reading ~/Desktop, ~/Documents and
# ~/Downloads unless /bin/bash is given Full Disk Access, so the schedule
# refuses those locations instead of failing silently every morning.
install-daily:
	@case "$(REPO)" in "$(HOME)/Desktop"*|"$(HOME)/Documents"*|"$(HOME)/Downloads"*) \
		echo "The repo is in a folder macOS hides from background jobs: $(REPO)"; \
		echo "Move it (e.g. to ~/gojobs) and run make install-daily from there."; exit 1;; esac
	mkdir -p logs $(HOME)/Library/LaunchAgents
	sed 's|__REPO__|$(REPO)|g' deploy/gojobs.daily.plist > $(PLIST_DST)
	launchctl bootout gui/$$(id -u)/$(PLIST_NAME) 2>/dev/null || true
	launchctl bootstrap gui/$$(id -u) $(PLIST_DST)
	@echo "Scheduled: every day at 09:00. Log: $(REPO)/logs/daily.log"

uninstall-daily:
	launchctl bootout gui/$$(id -u)/$(PLIST_NAME) 2>/dev/null || true
	rm -f $(PLIST_DST)
