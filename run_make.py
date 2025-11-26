#!/usr/bin/env python3
import subprocess
import sys
import os

os.chdir('/workspace/repo-76e8dc9d-020e-4ec1-93c2-ad0a593aa1a6')

# Run make oapi-generate
print("Running make oapi-generate...")
result = subprocess.run(['make', 'oapi-generate'], capture_output=True, text=True)
print(result.stdout)
if result.stderr:
    print("STDERR:", result.stderr, file=sys.stderr)
if result.returncode != 0:
    print(f"Command failed with exit code {result.returncode}")
    sys.exit(result.returncode)

print("\nGeneration complete!")
