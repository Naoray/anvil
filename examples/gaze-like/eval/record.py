# Synthetic stand-in: a real runner writes one JSONL file of detections.
import argparse, json, os
parser = argparse.ArgumentParser()
parser.add_argument("--out", required=True)
args = parser.parse_args()
os.makedirs(args.out, exist_ok=True)
with open(os.path.join(args.out, "detections.jsonl"), "w") as f:
    f.write(json.dumps({"doc": "doc-001.txt", "spans": []}) + "\n")
