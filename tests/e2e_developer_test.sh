#!/bin/bash
# End-to-end test for developer identity layer
# Requires: server running on localhost:8080, developer+agents registered from CLI tests

set -e

AGENT_ID="agdns:90423086d38f9753fb69badab6b1a581"
DEV_ID="agdns:dev:60df05562c88b7f50d6d7a4d8296fffa"
BINARY="./agentdns"

echo "=== E2E Developer Identity Tests ==="
echo ""

# Test 1: Health check
echo "Test 1: Health check"
HEALTH=$(curl -s http://localhost:8080/health)
echo "  Result: $HEALTH"
if echo "$HEALTH" | grep -q '"ok"'; then
    echo "  PASS"
else
    echo "  FAIL"
    exit 1
fi
echo ""

# Test 2: Get developer
echo "Test 2: Get developer record"
DEV=$(curl -s http://localhost:8080/v1/developers/$DEV_ID)
DEV_NAME=$(echo "$DEV" | python3 -c 'import json,sys; print(json.load(sys.stdin)["name"])')
echo "  Developer name: $DEV_NAME"
if [ "$DEV_NAME" = "Alice the Builder" ]; then
    echo "  PASS"
else
    echo "  FAIL"
    exit 1
fi
echo ""

# Test 3: Get agent with developer fields
echo "Test 3: Get agent with developer fields"
AGENT=$(curl -s http://localhost:8080/v1/agents/$AGENT_ID)
AGENT_DEV_ID=$(echo "$AGENT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("developer_id",""))')
AGENT_INDEX=$(echo "$AGENT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("agent_index",""))')
HAS_PROOF=$(echo "$AGENT" | python3 -c 'import json,sys; print("developer_proof" in json.load(sys.stdin))')
echo "  developer_id: $AGENT_DEV_ID"
echo "  agent_index: $AGENT_INDEX"
echo "  has_proof: $HAS_PROOF"
if [ "$AGENT_DEV_ID" = "$DEV_ID" ] && [ "$AGENT_INDEX" = "0" ] && [ "$HAS_PROOF" = "True" ]; then
    echo "  PASS"
else
    echo "  FAIL: developer fields missing or wrong"
    exit 1
fi
echo ""

# Test 4: List developer agents
echo "Test 4: List developer agents"
AGENTS=$(curl -s http://localhost:8080/v1/developers/$DEV_ID/agents)
COUNT=$(echo "$AGENTS" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("count",0))')
echo "  Agent count: $COUNT"
if [ "$COUNT" -ge "2" ]; then
    echo "  PASS"
else
    echo "  FAIL: expected at least 2 agents"
    exit 1
fi
echo ""

# Test 5: Search should find agent
echo "Test 5: Search finds developer agent"
SEARCH=$(curl -s -X POST http://localhost:8080/v1/search -H 'Content-Type: application/json' -d '{"query":"code review python security","max_results":5}')
FOUND=$(echo "$SEARCH" | python3 -c "import json,sys; r=json.load(sys.stdin); print(any(x['agent_id']=='$AGENT_ID' for x in r.get('results',[])))")
echo "  Found agent in search: $FOUND"
if [ "$FOUND" = "True" ]; then
    echo "  PASS"
else
    echo "  FAIL"
    exit 1
fi
echo ""

# Test 6: Developer not found should return 404
echo "Test 6: Unknown developer returns 404"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/developers/agdns:dev:nonexistent)
echo "  HTTP status: $STATUS"
if [ "$STATUS" = "404" ]; then
    echo "  PASS"
else
    echo "  FAIL"
    exit 1
fi
echo ""

# Test 7: Register agent without developer (backward compatibility)
echo "Test 7: Register agent WITHOUT developer (backward compat)"
# Try registering; it may already exist from a previous run (409 is okay)
$BINARY register \
  --name "StandaloneAgent" \
  --agent-url "https://standalone.example.com/agent.json" \
  --category "tools" \
  --tags "standalone" \
  --summary "Agent without developer identity" 2>&1 || true

# small delay for indexing
sleep 0.5

STANDALONE_SEARCH=$(curl -s -X POST http://localhost:8080/v1/search -H 'Content-Type: application/json' -d '{"query":"StandaloneAgent","max_results":5}')
STANDALONE_FOUND=$(echo "$STANDALONE_SEARCH" | python3 -c "
import json, sys
r = json.load(sys.stdin)
results = [x for x in r.get('results', []) if x['name'] == 'StandaloneAgent']
if results:
    dev_id = results[0].get('developer_id', '')
    print('True' if dev_id == '' else 'False')
else:
    print('NotFound')
")
echo "  Standalone agent found without developer_id: $STANDALONE_FOUND"
if [ "$STANDALONE_FOUND" = "True" ]; then
    echo "  PASS"
else
    echo "  FAIL: result=$STANDALONE_FOUND"
    exit 1
fi
echo ""

# Test 8: Duplicate developer registration should fail (bad sig = 401)
echo "Test 8: Duplicate developer registration fails"
DUP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/developers \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","public_key":"ed25519:hJBuB79vGqg5Mj1Iv2A/MmC+PpficWzRR3ETBssJnf4=","signature":"ed25519:AAAA"}')
echo "  HTTP status: $DUP_STATUS"
if [ "$DUP_STATUS" = "401" ] || [ "$DUP_STATUS" = "409" ]; then
    echo "  PASS (expected 401 or 409)"
else
    echo "  FAIL: expected 401 or 409, got $DUP_STATUS"
    exit 1
fi
echo ""

# Test 9: Agent registration with invalid developer proof should fail
echo "Test 9: Agent registration with invalid developer proof fails"
BAD_PROOF_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/agents \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"BadAgent",
    "agent_url":"https://bad.com/agent.json",
    "category":"tools",
    "public_key":"ed25519:isFWPUc4qn2BmxKh/td7nb8+1HRaNrVkgfFavPrH69A=",
    "signature":"ed25519:AAAA",
    "developer_id":"'"$DEV_ID"'",
    "developer_proof":{
      "developer_public_key":"ed25519:hJBuB79vGqg5Mj1Iv2A/MmC+PpficWzRR3ETBssJnf4=",
      "agent_index":99,
      "developer_signature":"ed25519:FAKE_SIGNATURE"
    }
  }')
echo "  HTTP status: $BAD_PROOF_STATUS"
if [ "$BAD_PROOF_STATUS" = "401" ]; then
    echo "  PASS (invalid proof rejected)"
else
    echo "  FAIL: expected 401, got $BAD_PROOF_STATUS"
    exit 1
fi
echo ""

# Test 10: Agent registration with non-existent developer should fail
echo "Test 10: Agent registration with non-existent developer fails"
NO_DEV_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/agents \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"OrphanAgent",
    "agent_url":"https://orphan.com/agent.json",
    "category":"tools",
    "public_key":"ed25519:isFWPUc4qn2BmxKh/td7nb8+1HRaNrVkgfFavPrH69A=",
    "signature":"ed25519:AAAA",
    "developer_id":"agdns:dev:nonexistent",
    "developer_proof":{
      "developer_public_key":"ed25519:FAKE",
      "agent_index":0,
      "developer_signature":"ed25519:FAKE"
    }
  }')
echo "  HTTP status: $NO_DEV_STATUS"
if [ "$NO_DEV_STATUS" = "400" ] || [ "$NO_DEV_STATUS" = "401" ]; then
    echo "  PASS (non-existent developer rejected with $NO_DEV_STATUS)"
else
    echo "  FAIL: expected 400 or 401, got $NO_DEV_STATUS"
    exit 1
fi
echo ""

echo "=== ALL TESTS PASSED ==="
