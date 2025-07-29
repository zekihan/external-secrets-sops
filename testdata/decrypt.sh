#!/bin/bash

set -e

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
cd "${scriptDir}" || exit 1

export SOPS_AGE_KEY_FILE="${scriptDir}/key.txt"

function decrypt() {
	local file
	local dest_file
	file=$1
	dest_file=$2
	sops --decrypt "${file}" >"${dest_file}"
}

function decrypt_file() {
	local file
	file=$1
	local dest_file
	dest_file="${file//generated/decrypted}"
	mkdir -p "$(dirname "${dest_file}")"
	
	echo "Decrypting: ${file}"
	decrypt "${file}" "${dest_file}"
}

echo "Starting decryption process..."

# Find all encrypted YAML files
find ./generated -type f \( -name "*.yaml" -o -name "*.yml" \) | while read -r file; do
	decrypt_file "${file}"
done

echo "Decryption process completed!"
echo ""
echo "Decrypted files structure:"
find ./decrypted -type f | sort
