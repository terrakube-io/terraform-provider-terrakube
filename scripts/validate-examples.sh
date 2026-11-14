#!/usr/bin/env bash
# Runs `terraform validate` (or `tofu validate`) on every example with the provider built from
# this checkout, so the examples stay valid. Each example directory is copied to a temporary
# directory next to examples/provider/provider.tf and validated on its own.
#
# Usage: scripts/validate-examples.sh <terraform|tofu> [example directory ...]
#
# The examples use ephemeral variables and write-only arguments, so the CLI must be Terraform
# 1.11 or later, or OpenTofu 1.11 or later.
set -uo pipefail

cli=${1:?usage: scripts/validate-examples.sh <terraform|tofu> [example directory ...]}
shift

cd "$(dirname "$0")/.."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
go build -o "$work/bin/terraform-provider-terrakube" .

# dev_overrides makes the CLI use the binary above and skips `init` for the provider.
cat > "$work/dev.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "terrakube-io/terrakube" = "$work/bin"
  }
  direct {}
}
EOF

if [ "$#" -gt 0 ]; then
  dirs=("$@")
else
  dirs=(examples/resources/*/ examples/data-sources/*/)
fi

failed=0
for dir in "${dirs[@]}"; do
  dir=${dir%/}
  name="$(basename "$(dirname "$dir")")/$(basename "$dir")"
  target="$work/examples/$name"
  mkdir -p "$target"
  cp "$dir"/*.tf "$target"/
  cp examples/provider/provider.tf "$target"/

  if output=$(cd "$target" && TF_CLI_CONFIG_FILE="$work/dev.tfrc" "$cli" validate -no-color 2>&1); then
    echo "ok   $name"
  else
    failed=$((failed + 1))
    echo "FAIL $name"
    # Skip the dev_overrides warning and show from the first error on.
    errors=$(echo "$output" | awk '/Error:/ {found = 1} found')
    echo "${errors:-$output}" | sed 's/^/     /'
  fi
done

if [ "$failed" -gt 0 ]; then
  echo
  echo "$failed example(s) failed validation"
  exit 1
fi
echo
echo "all examples are valid"
