CC      ?= cc
CFLAGS  ?= -O2 -Wall -Wextra -Wno-unused-parameter -Wno-missing-field-initializers
SRC     := main.c
BIN     := oracle

.PHONY: all clean regenerate update-clay verify

all: $(BIN)

$(BIN): $(SRC) clay.h
	$(CC) $(CFLAGS) -o $@ $(SRC)

# Regenerate every committed golden JSON file under ../testdata/ by running
# every scene through the oracle. Run this whenever clay.h is updated or
# scenes are added.
regenerate: $(BIN)
	@mkdir -p ../testdata
	@for s in $$(./$(BIN) --list); do \
		echo "  regen $$s.golden.json"; \
		./$(BIN) $$s > ../testdata/$$s.golden.json; \
	done

# Re-vendor clay.h from upstream and regenerate goldens, recording the exact
# commit in CLAY_VERSION. Run this when the Clay repo changes. Examples:
#   make update-clay REF=v0.15
#   make update-clay REF=e6cc369
#   make update-clay CLAY_SRC=/path/to/clay     # offline, from a local checkout
update-clay:
	@CLAY_SRC="$(CLAY_SRC)" ./update-clay.sh $(REF)

# Reproducibility guard for CI: a clean rebuild + regenerate must not change any
# committed golden. Fails loudly on a stale/hand-edited golden or a clay.h bump
# that was committed without regenerating. Offline (no network).
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
	@echo "verify: OK — committed goldens reproduce from a clean oracle build"

clean:
	rm -f $(BIN)
