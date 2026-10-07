#!/bin/bash
# Lokales Release: pruefen, testen, bauen, signieren, notarisieren, verpacken, veroeffentlichen.
# Aufruf: scripts/release.sh vX.Y.Z [--dry-run] [--check-only] [--publish vX.Y.Z]
#   --publish     ohne Rueckfrage veroeffentlichen (muss dem Tag entsprechen); sonst "y" am Terminal
#   --dry-run     Schritte 1-6 ohne notarytool/stapler/gh/Publish; signiert mit Developer ID, falls
#                 vorhanden, sonst ad-hoc
#   --check-only  nur die Pruefungen aus Schritt 1
set -euo pipefail

TEAM=H4MCP94STC
PROFILE=agentstatus-notary

USAGE="usage: $0 vX.Y.Z [--dry-run] [--check-only] [--publish vX.Y.Z]"
TAG=""; DRY=0; CHECK=0; PUBLISH=""; CONFTEST=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1 ;;
    --check-only) CHECK=1 ;;
    --confirm-test) CONFTEST=1 ;; # nur Selbsttest der Bestaetigung
    --publish) PUBLISH="${2:-}"; [ $# -ge 2 ] || { echo "$USAGE" >&2; exit 2; }; shift ;;
    *) if [[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] && [ -z "$TAG" ]; then TAG="$1"; else echo "$USAGE" >&2; exit 2; fi ;;
  esac
  shift
done
[ -n "$TAG" ] || { echo "$USAGE" >&2; exit 2; }
VER=${TAG#v}
VERRE=${VER//./\\.}
CLRE="^## \\[$VERRE\\]( - .*)?$"

die() { echo "release: $*" >&2; exit 1; }
[ -z "$PUBLISH" ] || [ "$PUBLISH" = "$TAG" ] || die "--publish $PUBLISH does not match $TAG"

# Veroeffentlichen nur mit --publish vX.Y.Z oder einem "y" vom Terminal; Pipes bestaetigen nie.
confirm_publish() {
  [ -n "$PUBLISH" ] && return 0
  [ -t 0 ] || return 1
  local ans
  read -r -p "Publish $TAG to GitHub? [y/N] " ans </dev/tty || return 1
  [ "$ans" = y ] || [ "$ans" = Y ]
}
if [ "$CONFTEST" = 1 ]; then
  if confirm_publish; then echo confirmed; exit 0; else echo "not confirmed"; exit 1; fi
fi

# Aufraeumen und Wiederherstellungshinweise bei Abbruch nach aussen
STAGE=""; NOTES=""
cleanup() {
  local rc=$?
  [ "$STAGE" = pushed ] || [ -z "$NOTES" ] || rm -f "$NOTES"
  if [ $rc -ne 0 ]; then
    case "$STAGE" in
      tagged) echo "release: failed after tagging. Nothing is public. Undo: git tag -d $TAG" >&2 ;;
      pushed) echo "release: main and tag $TAG are PUBLIC, but no GitHub release exists. Next: gh release create $TAG $ZIP --title $TAG --notes-file $NOTES (SHA-256: $SHA)" >&2 ;;
      released) echo "release: GitHub release $TAG is LIVE, but the cask is not updated. Next: cp dist/agentstatus.rb Casks/agentstatus.rb, commit and push main." >&2 ;;
      committed) echo "release: GitHub release $TAG is LIVE and the cask commit exists locally, but it is NOT pushed. Next: git push origin main" >&2 ;;
    esac
  fi
}
trap cleanup EXIT

cd "$(cd "$(dirname "$0")/.." && pwd)"

# 1. Pruefungen, bevor irgendetwas gebaut wird
[ -z "$(git status --porcelain)" ] || die "working tree not clean"
[ "$(git branch --show-current)" = main ] || die "not on branch main"
[ "$(tr -d '[:space:]' < VERSION)" = "$VER" ] || die "VERSION ($(tr -d '[:space:]' < VERSION)) does not match $VER"
git rev-parse -q --verify "refs/tags/$TAG" >/dev/null && die "tag $TAG already exists"
if [ "$DRY" = 0 ]; then
  [ -z "$(git ls-remote --tags origin "refs/tags/$TAG")" ] || die "tag $TAG already exists on origin"
fi
grep -Eq "$CLRE" CHANGELOG.md || die "CHANGELOG.md has no section [$VER]"

IDENTITY=$(security find-identity -v -p codesigning | grep "Developer ID Application" | grep "($TEAM)" | head -1 | sed 's/.*"\(.*\)".*/\1/' || true)
if [ -z "$IDENTITY" ]; then
  [ "$DRY" = 1 ] || die "no 'Developer ID Application' identity for team $TEAM in keychain"
  echo "notice: no Developer ID identity found, dry run signs ad-hoc" >&2
fi
if [ "$DRY" = 0 ]; then
  xcrun notarytool history --keychain-profile "$PROFILE" >/dev/null 2>&1 || die "notarytool profile '$PROFILE' missing or invalid"
  gh auth status >/dev/null 2>&1 || die "gh not authenticated"
fi
echo "checks ok ($TAG, ${IDENTITY:-ad-hoc})"
[ "$CHECK" = 0 ] || exit 0

# 2. Tests
go vet ./...
go test ./...
(cd panel && swift test)

# 3+4. Bauen und signieren (innen nach aussen, in build-app.sh)
rm -rf dist
if [ -n "$IDENTITY" ]; then
  scripts/build-app.sh --sign "$IDENTITY" --out dist
else
  scripts/build-app.sh --out dist
fi
APP=dist/AgentStatus.app
ZIP="dist/AgentStatus-$VER.zip"; SHA=""

# 5. Notarisieren und stapeln
if [ "$DRY" = 0 ]; then
  ditto -c -k --keepParent "$APP" dist/AgentStatus-notarize.zip
  result=$(xcrun notarytool submit dist/AgentStatus-notarize.zip --keychain-profile "$PROFILE" --wait --output-format json) || true
  status=$(printf '%s' "$result" | plutil -extract status raw -o - - 2>/dev/null) || status=unknown
  if [ "$status" != Accepted ]; then
    id=$(printf '%s' "$result" | plutil -extract id raw -o - - 2>/dev/null) || id=""
    if [ -n "$id" ]; then
      echo "Show the rejection log with: xcrun notarytool log $id --keychain-profile $PROFILE" >&2
      xcrun notarytool log "$id" --keychain-profile "$PROFILE" >&2 || true
    else
      echo "notarytool output: $result" >&2
    fi
    die "notarization failed (status: $status); nothing was published"
  fi
  xcrun stapler staple "$APP"
  spctl -a -vv -t exec "$APP" 2>&1 | grep -q "source=Notarized Developer ID" || die "spctl: not 'Notarized Developer ID'"
  rm -f dist/AgentStatus-notarize.zip
fi

# 6. Verpacken
ditto -c -k --keepParent "$APP" "$ZIP"
SHA=$(shasum -a 256 "$ZIP" | cut -d' ' -f1)
echo "$ZIP"
echo "sha256 $SHA"
sed -e "s/@VERSION@/$VER/" -e "s/@SHA256@/$SHA/" packaging/agentstatus.rb.tmpl > dist/agentstatus.rb

if [ "$DRY" = 1 ]; then
  echo "dry run done, nothing notarized or published"
  exit 0
fi

# 7. Veroeffentlichen, nur nach ausdruecklicher Bestaetigung
if ! confirm_publish; then
  echo "Not published. Rerun in a terminal or with --publish $TAG."
  exit 0
fi

NOTES=$(mktemp)
sed -E -n "/$CLRE/,/^## /{ /^## /!p; }" CHANGELOG.md > "$NOTES"
grep -q '[^[:space:]]' "$NOTES" || die "CHANGELOG notes for $VER are empty"
printf '\nSHA-256: %s\n' "$SHA" >> "$NOTES"

git tag "$TAG"; STAGE=tagged
git push --atomic origin main "refs/tags/$TAG"; STAGE=pushed
gh release create "$TAG" "$ZIP" --title "$TAG" --notes-file "$NOTES"; STAGE=released

# Cask liegt im Repo selbst (Tap = dieses Repo); die Pruefsumme gibt es erst nach dem Build.
mkdir -p Casks
cp dist/agentstatus.rb Casks/agentstatus.rb
git add Casks/agentstatus.rb
if git diff --cached --quiet; then
  echo "cask unchanged"
else
  git commit -m "chore: Cask fuer $TAG"; STAGE=committed
  git push origin main
fi
STAGE=done
