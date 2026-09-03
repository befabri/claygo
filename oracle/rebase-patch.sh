#!/usr/bin/env sh
# Rebase or edit an extension patch under patches/ without touching its hunks
# by hand. The patch is applied loosely onto its base, the result is edited as
# ordinary C, and the patch is re-diffed from that. The build keeps applying
# the committed patch strictly (--fuzz=0). Driven by `make rebase-patch` and
# `make refresh-patch`; see UPSTREAM.md, "Extensions".
#
# Usage, from oracle/:
#   ./rebase-patch.sh rebase  patches/NNNN-name.patch
#       Writes clay_ext.h.base (clay.h plus every patch sorted before this one,
#       applied strictly) and clay_ext.h.work (base plus this patch, applied
#       with fuzz). Hunks that still do not apply are left in
#       clay_ext.h.work.rej; fold them into clay_ext.h.work by hand, then
#       delete the .rej.
#   ./rebase-patch.sh refresh patches/NNNN-name.patch
#       Rewrites the patch as the diff from clay_ext.h.base to clay_ext.h.work,
#       keeping the text above the first hunk, proves the result applies
#       strictly and reproduces clay_ext.h.work, and removes the work files.
set -eu
export LC_ALL=C # byte-order globbing, the order the Makefile applies patches in

cd "$(dirname "$0")"
BASE=clay_ext.h.base
WORK=clay_ext.h.work
REJ=$WORK.rej

die() { echo "rebase-patch: $*" >&2; exit 1; }

[ $# -eq 2 ] || { echo "usage: $0 {rebase|refresh} patches/NNNN-name.patch" >&2; exit 2; }
mode=$1
target=$2
[ -n "$target" ] || die "no patch named. Pass P=patches/NNNN-name.patch (required when patches/ holds more than one)."
target=${target#./}
case $target in
    patches/*) ;;
    */*) die "$target: extension patches live under patches/" ;;
    *) target=patches/$target ;;
esac
[ -f "$target" ] || die "no such patch: $target"

apply_strict() { patch --silent --fuzz=0 --no-backup-if-mismatch "$1" < "$2"; }

case $mode in
rebase)
    [ ! -e "$WORK" ] || die "$WORK exists. Finish with 'make refresh-patch P=$target', or delete $BASE, $WORK and $REJ to start over."
    rm -f "$BASE" "$REJ"
    cp clay.h "$BASE"
    found=no
    for p in patches/*.patch; do
        if [ "$p" = "$target" ]; then found=yes; break; fi
        echo "  base += $p"
        apply_strict "$BASE" "$p" || die "$p sorts before $target and no longer applies strictly; rebase it first."
    done
    [ "$found" = yes ] || { rm -f "$BASE"; die "$target is not one of patches/*.patch"; }
    cp "$BASE" "$WORK"
    echo "  $WORK = base + $target (fuzz allowed)"
    if patch --fuzz=2 --no-backup-if-mismatch "$WORK" < "$target"; then
        echo "rebase-patch: every hunk applied. Edit $WORK, then run 'make refresh-patch P=$target'."
    else
        echo "rebase-patch: rejected hunks are in $REJ. Fold them into $WORK by hand, delete the .rej, then run 'make refresh-patch P=$target'." >&2
        exit 1
    fi
    ;;
refresh)
    [ -f "$BASE" ] && [ -f "$WORK" ] || die "no rebase in progress for $target: run 'make rebase-patch P=$target' first."
    [ ! -e "$REJ" ] || die "$REJ still exists: fold its hunks into $WORK, then delete it."
    tmp=$(mktemp "$target.XXXXXX")
    check=$(mktemp clay_ext.h.check.XXXXXX)
    trap 'rm -f "$tmp" "$check"' EXIT
    # Everything above the first hunk is the patch's own description; keep it.
    awk '/^--- /{exit} {print}' "$target" > "$tmp"
    rc=0
    diff -u -L a/clay.h -L b/clay.h "$BASE" "$WORK" >> "$tmp" || rc=$?
    case $rc in
        0) die "$BASE and $WORK are identical; refusing to write an empty patch." ;;
        1) ;;
        *) die "diff exited $rc" ;;
    esac
    # The refreshed patch must apply strictly to its base and rebuild WORK exactly.
    cp "$BASE" "$check"
    apply_strict "$check" "$tmp" || die "the refreshed patch does not apply strictly to $BASE; nothing written."
    cmp -s "$check" "$WORK" || die "the refreshed patch does not reproduce $WORK; nothing written."
    mv "$tmp" "$target"
    rm -f "$BASE" "$WORK"
    echo "rebase-patch: wrote $target ($(grep -c '^@@' "$target") hunks). Next: make verify, then commit it together with clay.h and ../testdata/."
    ;;
*)
    die "unknown mode '$mode' (rebase or refresh)"
    ;;
esac
