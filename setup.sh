#!/bin/bash
# Clone (or refresh) the three sibling repos as local build contexts.
# Run before: HOST_PORT=<port> docker compose up --build
set -e

clone_or_refresh() {
  local repo_url="$1"
  local dir="$2"
  if [ -d "$dir/.git" ]; then
    echo "Refreshing $dir from origin/main..."
    git -C "$dir" fetch origin
    git -C "$dir" checkout origin/main --detach
  elif [ -d "$dir" ] && [ -n "$(ls -A "$dir")" ]; then
    echo "Dir $dir exists with content — using as-is for build."
  else
    echo "Cloning $repo_url into $dir..."
    git clone --depth 1 --branch main "$repo_url" "$dir"
  fi
}

clone_or_refresh "https://github.com/duganbrettc/cascade-p3r5-bios-db.git"  db
clone_or_refresh "https://github.com/duganbrettc/cascade-p3r5-bios-api.git" api
clone_or_refresh "https://github.com/duganbrettc/cascade-p3r5-bios-web.git" web

echo "Setup complete. Run: HOST_PORT=8080 docker compose up --build"
