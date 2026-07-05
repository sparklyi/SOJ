#!/usr/bin/env bash
set -euo pipefail

if command -v runsc >/dev/null 2>&1 && docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -q '"runsc"'; then
  echo "gVisor runsc is already installed and registered with Docker"
  exit 0
fi

if ! command -v sudo >/dev/null 2>&1; then
  echo "sudo is required to install gVisor/runsc" >&2
  exit 1
fi

if ! command -v apt-get >/dev/null 2>&1; then
  echo "this helper currently supports Debian/Ubuntu apt systems; install runsc manually from https://gvisor.dev/docs/user_guide/install/" >&2
  exit 1
fi

sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg lsb-release

sudo install -d -m 0755 /usr/share/keyrings
curl -fsSL https://gvisor.dev/archive.key | sudo gpg --dearmor -o /usr/share/keyrings/gvisor-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" | sudo tee /etc/apt/sources.list.d/gvisor.list >/dev/null

sudo apt-get update
sudo apt-get install -y runsc
sudo runsc install

if command -v systemctl >/dev/null 2>&1; then
  sudo systemctl restart docker || true
else
  sudo service docker restart || true
fi

if ! docker info --format '{{json .Runtimes}}' | grep -q '"runsc"'; then
  echo "runsc installed, but Docker does not report the runsc runtime; restart Docker and rerun this script" >&2
  exit 1
fi

echo "gVisor runsc installed and registered with Docker"
