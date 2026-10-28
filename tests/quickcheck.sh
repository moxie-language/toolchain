#!/bin/bash
# The release gate: about ten minutes, not two and a half hours.
#
# tests/unitrun.sh --phases 0a,0b,2 is the regression sweep: 170 units, every
# fixture in the tree, ~2.5 h. That depth is for hunting a regression whose
# location is unknown. Once the compiler is expected to work, the question
# changes to "does this compiler still build usable code", which is a much
# smaller question with a much cheaper answer:
#
#   1. the bootstrap reproduces itself (phase 2)      the compiler's own test
#   2. a fixture set that touches every runtime edge   ~13 units, ~5 min
#      the copy rule, the codec/relocation path, the
#      crypto primitives and their encodings, the
#      compress and image paths, name parsing, headers
#   3. the tree is clean afterwards                    every unitrun run checks it
#
# The selection is deliberately one fixture per subsystem, chosen because a
# whole subsystem dies at once when the runtime regresses - which is exactly
# what the last four rounds kept finding. More than one fixture per subsystem
# buys coverage of that subsystem's own edge cases, and the full sweep is still
# there for that.
#
#   tests/quickcheck.sh              bootstrap + fixtures   (~10 min)
#   tests/quickcheck.sh --fast       fixtures only         (~5 min)
#   tests/quickcheck.sh --fixtures   skip the fixtures, bootstrap only
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

FIXTURES=(
	hash_basic          # sha2/sha3/md5/sha1 + hmac, over the byteorder reads
	poly1305_aead       # ChaCha20-Poly1305, RFC 8439 vectors, binary PutUint*
	mlkem_vectors       # FIPS 203 keygen/encaps/decaps, bigmod limbs
	asn1_builder_der    # DER encoding at offsets
	asn1_oid_der        # OID parsing, string/int conversions
	jpeg_progressive_cmyk # fdct/idct, the scan destination, the Huffman tables
	dns_name_roundtrip  # dnsmessage header patch, net name compression
	slice_copy_rule     # the copy rule itself: ten assertions about sub-slices
	rand_fill           # crypto/rand and the DRBG fill what they are given
	mime_header_api     # textproto canonicalisation and header folding
	gosym_pclntab       # the codec/relocation path over tables
	bytes_replacer      # the trie builder, generic push/resize
	bessel_large_x       # math/big float formatting through ftoa
)

DO_FIXTURES=1
DO_SELFHOST=1
for arg in "$@"; do
	case "$arg" in
	--fast) DO_SELFHOST=0 ;;
	--fixtures) DO_SELFHOST=0 ;;
	--bootstrap) DO_FIXTURES=0 ;;
	-h | --help)
		sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown argument: $arg" >&2
		exit 2
		;;
	esac
done

JOINED="$(IFS=,; echo "${FIXTURES[*]}")"
rc=0

if [ "$DO_SELFHOST" = 1 ]; then
	echo "=== quickcheck: bootstrap ==="
	bash "$SCRIPT_DIR/unitrun.sh" -j 1 --phases 2 --force || rc=1
fi

if [ "$DO_FIXTURES" = 1 ]; then
	echo ""
	echo "=== quickcheck: fixtures ==="
	bash "$SCRIPT_DIR/unitrun.sh" -j 4 --phases 0a --units "$JOINED" --force || rc=1
fi

echo ""
if [ "$rc" = 0 ]; then
	echo "quickcheck: OK"
else
	echo "quickcheck: FAILED - run tests/unitrun.sh --phases 0a,0b,2 --force for the full sweep"
fi
exit "$rc"
