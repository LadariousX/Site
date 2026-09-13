#!/bin/bash

# Load this script's own config (remote host/paths) — not the app's .env or
# deploy/.env (the tunnel token), see readme.
if [[ -f .env ]]; then
    export $(grep -v '^#' .env | xargs)
else
    echo "Error: .env file not found."
    exit 1
fi

EXCLUDE_FROM="$(dirname "$0")/rsync_ignore.txt"

# --- SELECTION MENU ---
declare -A CHECKED
CHECKED[1]=true   # Restart remote compose
CHECKED[2]=true  # Rsync to remote
CHECKED[3]=false  # Backup to iCloud

LABELS=(
    "Restart remote Docker Compose"
    "Rsync to remote"
    "Backup to iCloud"
)

draw_menu() {
    clear
    echo "---------------------------------------"
    echo "        SYNCHRONIZE OPERATIONS"
    echo "---------------------------------------"
    for i in 1 2 3; do
        if [[ "${CHECKED[$i]}" == true ]]; then
            box="[x]"
        else
            box="[ ]"
        fi
        echo "$i) $box ${LABELS[$((i - 1))]}"
    done
    echo "---------------------------------------"
    echo "Press 1-3 to toggle, Enter to run"
}

while true; do
    draw_menu
    IFS= read -rsn1 key
    case "$key" in
        1|2|3)
            if [[ "${CHECKED[$key]}" == true ]]; then
                CHECKED[$key]=false
            else
                CHECKED[$key]=true
            fi
            ;;
        "")
            break
            ;;
    esac
done

# Build the execution queue. Compose restart always runs last, regardless
# of the order the boxes were checked in.
QUEUE=()
[[ "${CHECKED[2]}" == true ]] && QUEUE+=("rsync_remote")
[[ "${CHECKED[3]}" == true ]] && QUEUE+=("backup_icloud")
[[ "${CHECKED[1]}" == true ]] && QUEUE+=("restart_compose")

if [[ ${#QUEUE[@]} -eq 0 ]]; then
    echo "No operations selected. Exiting."
    exit 0
fi

# --- EXECUTION ---
echo "---------------------------------------"
echo " Queued: ${QUEUE[*]}"
echo "---------------------------------------"

for op in "${QUEUE[@]}"; do
    case "$op" in
        rsync_remote)
            DEST="$REMOTE_USER@$REMOTE_HOST:$REMOTE_PATH"
            echo "---------------------------------------"
            echo "[PUSH] Local --> Remote"
            echo "FROM: $LOCAL_PATH"
            echo "TO:   $DEST"
            echo "---------------------------------------"

            rsync -avzP --delete \
                -e "ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=3" \
                --exclude-from="$EXCLUDE_FROM" \
                "$LOCAL_PATH" "$DEST"

            echo "✓ Remote sync complete"
            ;;
        backup_icloud)
            echo "---------------------------------------"
            echo "[BACKUP] Local --> iCloud"
            echo "FROM: $LOCAL_PATH"
            echo "TO:   $BU_PATH"
            echo "---------------------------------------"

            rsync -avzP --delete \
                "$LOCAL_PATH" "$BU_PATH"

            echo "✓ iCloud backup complete"
            ;;
        restart_compose)
            echo "---------------------------------------"
            echo "Restarting Docker on remote server..."

            ssh "$REMOTE_USER@$REMOTE_HOST" << EOF
                cd "$REMOTE_PATH/deploy" || exit
                docker compose down
                docker compose up -d
EOF

            echo "✓ Site compose restarted"
            ;;
    esac
done

echo "---------------------------------------"
echo "✓ All operations complete"
echo "---------------------------------------"
