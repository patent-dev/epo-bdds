#!/usr/bin/env bash
#
# refresh-fixtures rebuilds the committed golden set in testdata/examples from the
# live demo recordings in demo/examples. Run it (after `make examples`) ONLY when a
# human intends to update the goldens to a newer real response.
#
# Unlike TIPO/JPO (whose demos already write the {endpoint,request,response}
# envelope as flat *.json), the BDDS demo records the wrapper's parsed result as
# demo/examples/<name>/response.json. This script wraps each of those bodies in the
# same envelope the deterministic tests (TestFixtures in decode_examples_test.go)
# read, so the golden shape stays identical to the other libraries.
#
# The deterministic tests read the COMMITTED testdata/examples copies, never
# demo/examples, so re-running the demo does not change test behaviour until the
# goldens are refreshed here and the diff is reviewed and committed.
set -euo pipefail

src="demo/examples"
dst="testdata/examples"
mkdir -p "$dst"

python3 - "$src" "$dst" <<'PY'
import json, os, sys
src, dst = sys.argv[1], sys.argv[2]

# (fixture filename, recorded folder, request description). Mirrors the demo's
# recording order; see demo/examples_record.go.
specs = [
    ("01-ListProducts.json",      "list_products",      "GET /products"),
    ("02-GetProduct.json",        "get_product",        "GET /products/{id}"),
    ("03-GetLatestDelivery.json", "get_latest_delivery", "latest delivery for a product"),
    ("04-DownloadFile.json",      "download_file",      "GET /products/{p}/deliveries/{d}/files/{f}"),
]

for fname, folder, req in specs:
    body_path = os.path.join(src, folder, "response.json")
    with open(body_path) as f:
        body = json.load(f)
    req_path = os.path.join(src, folder, "request.txt")
    if os.path.exists(req_path):
        with open(req_path) as f:
            req = f.read().strip() or req
    env = {
        "endpoint": folder,
        "request": {"description": req},
        "response": {"status": 200, "body": body},
    }
    with open(os.path.join(dst, fname), "w") as f:
        json.dump(env, f, indent=2, ensure_ascii=False)
        f.write("\n")
    print(f"wrote {fname}")
PY

echo "testdata/examples updated from demo/examples; review the diff before committing."
