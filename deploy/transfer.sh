#!/bin/bash

# --- GLOBAL SETTINGS ---
IGNORE_FILE="rsync_ignore.txt"
USER="your_username"  # Your remote server username

# --- SETUP 1: WORK FILES ---
HOST1="work-server.local"
LOCAL_PATH1="/Users/name/Documents/Work/"
REMOTE_PATH1="/home/user/backups/work/"

# --- SETUP 2: MEDIA/PERSONAL ---
HOST2="home-server.local"
LOCAL_PATH2="/Users/name/Movies/"
REMOTE_PATH2="/mnt/storage/media/"

# --- SELECTION MENU ---
echo "Select the sync profile:"
echo "1) Sync Work to $HOST1"
echo "2) Sync Media to $HOST2"
read -p "Enter choice [1 or 2]: " CHOICE

case $CHOICE in
    1)
        SRC="$LOCAL_PATH1"
        DEST="$USER@$HOST1:$REMOTE_PATH1"
        ;;
    2)
        SRC="$LOCAL_PATH2"
        DEST="$USER@$HOST2:$REMOTE_PATH2"
        ;;
    *)
        echo "Invalid selection. Exiting."
        exit 1
        ;;
esac

# Check for ignore file
if [[ ! -f "$IGNORE_FILE" ]]; then
    echo "Warning: $IGNORE_FILE not found. Proceeding without excludes."
    EXCLUDE_CMD=""
else
    EXCLUDE_CMD="--exclude-from=$IGNORE_FILE"
fi

# --- EXECUTION ---
echo "Syncing $SRC to $DEST..."
# Remove --dry-run below once you verify the paths are correct
rsync -avzP --delete \
    $EXCLUDE_CMD \
    --dry-run \
    "$SRC" "$DEST"

echo "Process Complete."
