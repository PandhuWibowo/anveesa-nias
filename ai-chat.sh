#!/usr/bin/env bash
# ai-chat.sh — CLI wrapper for the Nias AI chat endpoint (powered by SumoPod)
#
# Usage:
#   ./ai-chat.sh "Your question here"
#   ./ai-chat.sh -c 3 "Follow-up question"   # keep last N exchanges as context
#   ./ai-chat.sh -p /path/to/file "question" # inject a file as project context
#   ./ai-chat.sh --project "question"        # auto-inject README.md as context
#
# Config (env vars, or edit defaults below):
#   NIAS_URL      server base URL   (default: http://localhost:8080)
#   NIAS_USER     admin username    (default: admin)
#   NIAS_PASS     admin password    (default: Admin123!)
#
# Requires: curl, jq

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
NIAS_URL="${NIAS_URL:-http://localhost:8080}"
NIAS_USER="${NIAS_USER:-admin}"
NIAS_PASS="${NIAS_PASS:-Admin123!}"
TOKEN_FILE="${TMPDIR:-/tmp}/.nias_token"
HISTORY_FILE="${TMPDIR:-/tmp}/.nias_history"
CONTEXT_TURNS=0   # number of prior exchanges to include (0 = no history)
PROJECT_CONTEXT_FILE=""  # optional file to inject as system context

# ── Helpers ───────────────────────────────────────────────────────────────────
die()  { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" &>/dev/null || die "'$1' is required (brew install $1)"; }

need curl
need jq

# ── Argument parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    -c|--context) CONTEXT_TURNS="$2"; shift 2 ;;
    -p|--project-file)
      [[ -z "${2:-}" ]] && die "--project-file requires a path argument"
      PROJECT_CONTEXT_FILE="$2"; shift 2 ;;
    --project)
      # Auto-detect README.md relative to this script
      SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
      if [[ -f "$SCRIPT_DIR/README.md" ]]; then
        PROJECT_CONTEXT_FILE="$SCRIPT_DIR/README.md"
      else
        die "README.md not found at $SCRIPT_DIR — use --project-file <path> instead"
      fi
      shift ;;
    --clear)      rm -f "$HISTORY_FILE"; echo "History cleared."; exit 0 ;;
    --logout)     rm -f "$TOKEN_FILE";   echo "Token removed.";   exit 0 ;;
    -h|--help)
      sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    --) shift; break ;;
    -*) die "Unknown option: $1" ;;
    *)  break ;;
  esac
done

[[ $# -eq 0 ]] && die "No question provided. Usage: $0 \"Your question\""
QUESTION="$*"

# ── Auth: reuse cached token or log in ────────────────────────────────────────
get_token() {
  local resp
  resp=$(curl -sf -X POST "$NIAS_URL/api/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"$NIAS_USER\",\"password\":\"$NIAS_PASS\"}") \
    || die "Login request failed — is the server running at $NIAS_URL?"

  local tok
  tok=$(echo "$resp" | jq -r '.token // empty')
  [[ -z "$tok" ]] && die "Login failed: $(echo "$resp" | jq -r '.error // .message // .')"
  echo "$tok"
}

if [[ -f "$TOKEN_FILE" ]]; then
  TOKEN=$(cat "$TOKEN_FILE")
  # Quick validity check — /api/auth/me returns 401 if expired
  if ! curl -sf -o /dev/null "$NIAS_URL/api/auth/me" \
       -H "Authorization: Bearer $TOKEN" 2>/dev/null; then
    TOKEN=$(get_token)
    echo "$TOKEN" > "$TOKEN_FILE"
  fi
else
  TOKEN=$(get_token)
  echo "$TOKEN" > "$TOKEN_FILE"
fi

# ── Build message array (optional history) ────────────────────────────────────
MESSAGES="[]"

# Inject project context as a system message if requested
if [[ -n "$PROJECT_CONTEXT_FILE" ]]; then
  [[ -f "$PROJECT_CONTEXT_FILE" ]] || die "Project context file not found: $PROJECT_CONTEXT_FILE"
  PROJECT_CONTENT=$(cat "$PROJECT_CONTEXT_FILE")
  # Truncate to ~8000 chars to stay within token limits
  PROJECT_CONTENT="${PROJECT_CONTENT:0:8000}"
  MESSAGES=$(echo "$MESSAGES" | jq --arg c "$PROJECT_CONTENT" \
    '. + [{"role": "system", "content": $c}]')
fi

if [[ "$CONTEXT_TURNS" -gt 0 && -f "$HISTORY_FILE" ]]; then
  # Each line in history: role<TAB>content
  # Take last N*2 lines (N user + N assistant turns)
  LIMIT=$(( CONTEXT_TURNS * 2 ))
  while IFS=$'\t' read -r role content; do
    MESSAGES=$(echo "$MESSAGES" | jq --arg r "$role" --arg c "$content" \
      '. + [{"role": $r, "content": $c}]')
  done < <(tail -n "$LIMIT" "$HISTORY_FILE")
fi

MESSAGES=$(echo "$MESSAGES" | jq --arg q "$QUESTION" \
  '. + [{"role": "user", "content": $q}]')

# ── Call /api/ai/chat ─────────────────────────────────────────────────────────
PAYLOAD=$(jq -n --argjson msgs "$MESSAGES" '{"messages": $msgs}')

RESPONSE=$(curl -sf -X POST "$NIAS_URL/api/ai/chat" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d "$PAYLOAD") \
  || die "AI request failed — check server logs"

# ── Extract and print the reply ───────────────────────────────────────────────
REPLY=$(echo "$RESPONSE" | jq -r '.choices[0].message.content // empty')

if [[ -z "$REPLY" ]]; then
  # Surface any error message from the AI provider
  ERR=$(echo "$RESPONSE" | jq -r '.error.message // .error // .' 2>/dev/null || echo "$RESPONSE")
  die "Empty response from AI: $ERR"
fi

echo ""
echo "$REPLY"
echo ""

# ── Persist to history (for -c context flag) ──────────────────────────────────
if [[ "$CONTEXT_TURNS" -gt 0 ]]; then
  printf "%s\t%s\n" "user"      "$QUESTION" >> "$HISTORY_FILE"
  printf "%s\t%s\n" "assistant" "$REPLY"    >> "$HISTORY_FILE"
fi
