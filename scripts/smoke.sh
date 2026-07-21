#!/bin/sh
set -eu

stamp_smoke_dir=$(mktemp -d "${TMPDIR:-/tmp}/stamp-smoke.XXXXXX")
trap 'rm -rf "$stamp_smoke_dir"' EXIT

bin/stamp project create "$stamp_smoke_dir/Annual Pack" --name "Annual Pack"
cp testdata/mixed/letter.doc.md "$stamp_smoke_dir/Annual Pack/documents/"
cp testdata/mixed/model.fods "$stamp_smoke_dir/Annual Pack/spreadsheets/"
bin/stamp preview --dir "$stamp_smoke_dir/Annual Pack"

test -s "$stamp_smoke_dir/Annual Pack/outputs/theme/examples/welcome-page.pdf"
test -s "$stamp_smoke_dir/Annual Pack/outputs/theme/examples/welcome-deck.pdf"
test -s "$stamp_smoke_dir/Annual Pack/outputs/documents/letter.pdf"
test -s "$stamp_smoke_dir/Annual Pack/outputs/spreadsheets/model.xlsx"
echo "Mixed document pack rendered successfully."
