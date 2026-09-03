CC      ?= cc
CFLAGS  ?= -O2 -Wall -Wextra -Wno-unused-parameter -Wno-missing-field-initializers
SRC     := main.c
BIN     := oracle
BIN_UPSTREAM := oracle-upstream
PATCHES := $(sort $(wildcard patches/*.patch))
EXT_HEADERS := $(sort $(wildcard patches/*.h))

.PHONY: all clean regenerate update-clay verify rebase-patch refresh-patch

all: $(BIN) $(BIN_UPSTREAM)

# clay_ext.h is the verbatim clay.h plus every patch under patches/, applied in
# name order. Each patch is dry-run first so a rejected hunk is reported (with
# its number and the offending file) before anything is written. Fuzz is
# disabled: a patch that only applies loosely is a patch that needs a rebase.
clay_ext.h: clay.h $(PATCHES)
	@cp clay.h clay_ext.h.tmp
	@for p in $(PATCHES); do \
		echo "  patch $$p"; \
		if ! patch --dry-run --silent --fuzz=0 --no-backup-if-mismatch clay_ext.h.tmp < $$p; then \
			echo "clay_ext.h: FAILED — $$p does not apply to clay.h."; \
			echo "Upstream changed code the extension patches. Rebase it: make rebase-patch P=$$p, edit clay_ext.h.work, make refresh-patch P=$$p (oracle/UPSTREAM.md, Extensions)."; \
			rm -f clay_ext.h.tmp; exit 1; \
		fi; \
		patch --silent --fuzz=0 --no-backup-if-mismatch clay_ext.h.tmp < $$p; \
	done
	@mv clay_ext.h.tmp clay_ext.h

# The patched oracle: every scene, upstream and extension. clay_ext.h
# #includes patches/*.h, so the binary depends on them directly.
$(BIN): $(SRC) clay_ext.h $(EXT_HEADERS)
	$(CC) $(CFLAGS) -o $@ $(SRC)

# The verbatim oracle: built straight from clay.h, lists only upstream scenes.
$(BIN_UPSTREAM): $(SRC) clay.h
	$(CC) $(CFLAGS) -DCLAY_ORACLE_UPSTREAM -o $@ $(SRC)

# Regenerate every committed golden JSON file under ../testdata/. Upstream
# scenes come from the verbatim binary, extension scenes (prefix ext_) from the
# patched one. Run this whenever clay.h, a patch, or the scene corpus changes.
regenerate: $(BIN) $(BIN_UPSTREAM)
	@mkdir -p ../testdata
	@for s in $$(./$(BIN_UPSTREAM) --list); do \
		echo "  regen $$s.golden.json"; \
		./$(BIN_UPSTREAM) $$s > ../testdata/$$s.golden.json; \
	done
	@for s in $$(./$(BIN) --list | grep '^ext_'); do \
		echo "  regen $$s.golden.json (extension)"; \
		./$(BIN) $$s > ../testdata/$$s.golden.json; \
	done

# Re-vendor clay.h from upstream and regenerate goldens, recording the exact
# commit in CLAY_VERSION. Run this when the Clay repo changes. Examples:
#   make update-clay REF=v0.15
#   make update-clay REF=e6cc369
#   make update-clay CLAY_SRC=/path/to/clay     # offline, from a local checkout
update-clay:
	@CLAY_SRC="$(CLAY_SRC)" ./update-clay.sh $(REF)

# Edit or rebase an extension patch without touching its hunks by hand.
# rebase-patch applies P loosely onto its base (clay.h plus the patches that
# sort before it) and leaves the result in clay_ext.h.work, with any rejected
# hunks in clay_ext.h.work.rej, for editing as ordinary C. refresh-patch
# re-diffs clay_ext.h.work into P with current line numbers and context and
# proves the result applies with --fuzz=0. P defaults to the only patch under
# patches/ and must be given once there are several. See UPSTREAM.md.
P ?= $(if $(filter 1,$(words $(PATCHES))),$(PATCHES),)
rebase-patch:
	@./rebase-patch.sh rebase "$(P)"
refresh-patch:
	@./rebase-patch.sh refresh "$(P)"

# Reproducibility guard for CI: a clean rebuild + regenerate must not change any
# committed golden. Fails loudly on a stale/hand-edited golden or a clay.h bump
# that was committed without regenerating. Offline (no network).
#
# It also proves the extension patch leaves upstream behavior alone: the
# verbatim binary must list no ext_ scene, the patched binary must list every
# upstream scene, and the patched binary must reproduce every upstream golden
# byte for byte. (Needs: cc, make, patch, git.)
verify:
	@$(MAKE) clean
	@$(MAKE) all
	@$(MAKE) regenerate
	@dirty="$$(git -C .. status --porcelain -- testdata/)"; \
	if [ -n "$$dirty" ]; then \
		echo "verify: FAILED — regenerated goldens differ from what is committed:"; \
		echo "$$dirty"; \
		echo "Run 'make -C oracle regenerate' and commit, or fix the divergence."; \
		exit 1; \
	fi
	@if ./$(BIN_UPSTREAM) --list | grep -q '^ext_'; then \
		echo "verify: FAILED — $(BIN_UPSTREAM) lists extension (ext_) scenes"; exit 1; \
	fi
	@for s in $$(./$(BIN_UPSTREAM) --list); do \
		if ! ./$(BIN) --list | grep -qx "$$s"; then \
			echo "verify: FAILED — $(BIN) does not list upstream scene $$s"; exit 1; \
		fi; \
		if ! ./$(BIN) $$s | cmp -s - ../testdata/$$s.golden.json; then \
			echo "verify: FAILED — the patched $(BIN) changes upstream scene $$s"; exit 1; \
		fi; \
	done
	@echo "verify: OK — committed goldens reproduce from a clean oracle build; the extension patch leaves upstream scenes unchanged"

clean:
	rm -f $(BIN) $(BIN_UPSTREAM) clay_ext.h clay_ext.h.tmp
