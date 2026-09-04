#!/bin/bash

# Load environment variables
if [[ -f .env ]]; then
    export $(grep -v '^#' .env | xargs)
else
    echo "Error: .env file not found."
    exit 1
fi

EXCLUDE_FROM="$(dirname "$0")/rsync_ignore.txt"

# --- SELECTION MENU ---
echo "---------------------------------------"
echo "        RSYNC DIRECTION MENU"
echo "---------------------------------------"
echo "1) [PUSH]          Local --> Remote"
echo "2) [BACKUP]        Local --> iCloud"
echo "3) [PUSH + BACKUP] Local --> Remote + iCloud"
echo "---------------------------------------"
read -p "Select option [1, 2, 3]: " CHOICE

SYNC_LOCAL=false
SYNC_REMOTE=true


case $CHOICE in
    1)
        NAME="PUSH"
        SRC="$LOCAL_PATH"
        DEST="$REMOTE_USER@$REMOTE_HOST:$REMOTE_PATH"
        ;;
    2)
       NAME="ICLOUD BACKUP"
       SRC="$LOCAL_PATH"
       SYNC_LOCAL=true
       SYNC_REMOTE=false
       BU_DEST="$BU_PATH"
        ;;
    3)
        NAME="PUSH + ICLOUD BACKUP"
        SRC="$LOCAL_PATH"
        DEST="$REMOTE_USER@$REMOTE_HOST:$REMOTE_PATH"
        SYNC_LOCAL=true
        BU_DEST="$BU_PATH"
        ;;
    *)
        echo "Invalid selection. Exiting."
        exit 1
        ;;
esac

# --- EXECUTION ---
echo "---------------------------------------"
echo " Executing $NAME..."
echo "FROM: $SRC"

if [[ "$SYNC_REMOTE" == true ]]; then
    echo "TO:   $DEST"
    echo "---------------------------------------"

    rsync -avzP --delete \
        -e "ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=3" \
        --exclude-from="$EXCLUDE_FROM" \
        "$SRC" "$DEST"

    echo "---------------------------------------"
    echo "✓ Remote sync complete"
    echo "Restarting Docker on remote server..."

    ssh "$REMOTE_USER@$REMOTE_HOST" << EOF
        cd "$REMOTE_PATH/deploy" || exit
        docker compose down
        docker compose up -d
EOF

    echo "Site compose restarted"
fi

if [[ "$SYNC_LOCAL" == true ]]; then
    echo "---------------------------------------"
    echo "Starting iCloud backup..."
    echo "TO:   $BU_DEST"
    echo "---------------------------------------"

    rsync -avzP --delete \
        "$SRC" "$BU_DEST"

    echo "---------------------------------------"
    echo "✓ iCloud backup complete"
fi

echo "---------------------------------------"
echo "✓ $NAME Complete"
echo "---------------------------------------"