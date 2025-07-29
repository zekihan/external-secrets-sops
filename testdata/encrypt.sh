#!/bin/bash

set -e

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
cd "${scriptDir}" || exit 1

export SOPS_AGE_KEY_FILE="${scriptDir}/key.txt"

function encrypt() {
	local file
	local dest_file
	file=$1
	dest_file=$2
	rm -f "${dest_file}"
	sops --encrypt "${file}" >"${dest_file}"
}

function decrypt() {
	local file
	local dest_file
	file=$1
	dest_file=$2
	sops --decrypt "${file}" >"${dest_file}"
}

function encrypt_if_necessary() {
	local file
	file=$1
	local dest_file
	dest_file="${file//src/generated}"
	mkdir -p "$(dirname "${dest_file}")"
	
	echo "Processing: ${file}"
	
	if [ -f "${dest_file}" ]; then
		local tmp_file
		tmp_file="$(mktemp)"
		decrypt "${dest_file}" "${tmp_file}"
		if ! cmp --silent -- "${tmp_file}" "${file}"; then
			echo "  → Encrypting (content changed)"
			encrypt "${file}" "${dest_file}"
		else
			echo "  → Skipping (no changes)"
		fi
		rm -f "${tmp_file}"
	else
		echo "  → Encrypting (new file)"
		encrypt "${file}" "${dest_file}"
	fi
}

echo "Starting encryption process..."

# Find all YAML files in src directory
find ./src -type f \( -name "*.yaml" -o -name "*.yml" \) | while read -r file; do
	encrypt_if_necessary "${file}"
done

echo "Encryption process completed!"
echo ""
echo "Generated files structure:"
find ./generated -type f | sort
