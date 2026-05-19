CC      ?= cc
CFLAGS  ?= -O2 -Wall -Wextra -Wno-unused-parameter -Wno-missing-field-initializers
SRC     := main.c
BIN     := oracle

.PHONY: all clean regenerate

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

clean:
	rm -f $(BIN)
