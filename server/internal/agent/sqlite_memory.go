package agent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"stackchan-gateway/internal/session"
)

const sqliteTimeFormat = time.RFC3339Nano

type SQLiteMemoryStore struct {
	db              *sql.DB
	recentTurnLimit int
}

func NewSQLiteMemoryStore(path string) (*SQLiteMemoryStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("memory sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create memory db dir: %w", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open memory sqlite db: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteMemoryStore{db: db, recentTurnLimit: defaultRecentTurnStoreLimit}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteMemoryStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteMemoryStore) SetRecentTurnLimit(maxPerDevice int) {
	if s == nil {
		return
	}
	if maxPerDevice <= 0 {
		maxPerDevice = defaultRecentTurnStoreLimit
	}
	s.recentTurnLimit = maxPerDevice
}

func (s *SQLiteMemoryStore) Upsert(ctx context.Context, memory Memory) (Memory, error) {
	if s == nil || s.db == nil {
		return Memory{}, fmt.Errorf("memory store is not open")
	}
	memory = normalizeMemory(memory)
	if err := validateMemory(memory); err != nil {
		return Memory{}, err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memories (
  id, user_id, device_id, session_id, type, content, importance, confidence,
  created_at, updated_at, last_used_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  user_id=excluded.user_id,
  device_id=excluded.device_id,
  session_id=excluded.session_id,
  type=excluded.type,
  content=excluded.content,
  importance=excluded.importance,
  confidence=excluded.confidence,
  updated_at=excluded.updated_at,
  last_used_at=excluded.last_used_at,
  metadata_json=excluded.metadata_json
`, memory.ID, memory.UserID, memory.DeviceID, memory.SessionID, memory.Type, memory.Content, memory.Importance, memory.Confidence,
		formatTime(memory.CreatedAt), formatTime(memory.UpdatedAt), formatNullableTime(memory.LastUsedAt), memory.MetadataJSON)
	if err != nil {
		return Memory{}, fmt.Errorf("upsert memory: %w", err)
	}
	return memory, nil
}

func (s *SQLiteMemoryStore) Retrieve(ctx context.Context, query MemoryQuery) ([]Memory, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("memory store is not open")
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, device_id, session_id, type, content, importance, confidence, created_at, updated_at, last_used_at, metadata_json
FROM memories
WHERE
  (? = '' OR user_id = '' OR user_id = ?)
  AND (? = '' OR device_id = '' OR device_id = ?)
ORDER BY importance DESC, updated_at DESC, created_at DESC
LIMIT ?
`, strings.TrimSpace(query.UserID), strings.TrimSpace(query.UserID), strings.TrimSpace(query.DeviceID), strings.TrimSpace(query.DeviceID), limit)
	if err != nil {
		return nil, fmt.Errorf("retrieve memories: %w", err)
	}
	defer rows.Close()

	memories := make([]Memory, 0, limit)
	for rows.Next() {
		var memory Memory
		var createdAt, updatedAt, lastUsedAt string
		if err := rows.Scan(&memory.ID, &memory.UserID, &memory.DeviceID, &memory.SessionID, &memory.Type, &memory.Content, &memory.Importance, &memory.Confidence, &createdAt, &updatedAt, &lastUsedAt, &memory.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		memory.CreatedAt = parseTime(createdAt)
		memory.UpdatedAt = parseTime(updatedAt)
		memory.LastUsedAt = parseTime(lastUsedAt)
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}
	if len(memories) > 0 {
		ids := make([]string, 0, len(memories))
		for _, memory := range memories {
			ids = append(ids, memory.ID)
		}
		_ = s.touchLastUsed(ctx, ids, time.Now())
	}
	return memories, nil
}

func (s *SQLiteMemoryStore) Delete(ctx context.Context, id string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("memory store is not open")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, fmt.Errorf("memory id is required")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete memory: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete memory rows affected: %w", err)
	}
	return affected > 0, nil
}

func (s *SQLiteMemoryStore) RecordConversationTurn(ctx context.Context, request session.ConversationTurnRecordRequest) error {
	if s == nil {
		return nil
	}
	return s.AppendRecentTurn(ctx, RecentTurn{
		SessionID:     request.SessionID,
		DeviceID:      request.DeviceID,
		Generation:    request.Generation,
		UserText:      request.UserText,
		AssistantText: request.AssistantText,
		CreatedAt:     request.CreatedAt,
	})
}

func (s *SQLiteMemoryStore) AppendRecentTurn(ctx context.Context, turn RecentTurn) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memory store is not open")
	}
	deviceID := strings.TrimSpace(turn.DeviceID)
	userText := compactRecentText(turn.UserText, maxRecentUserRunes)
	assistantText := compactRecentText(turn.AssistantText, maxRecentAssistantRunes)
	if deviceID == "" || userText == "" || assistantText == "" {
		return nil
	}
	createdAt := turn.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO recent_turns (device_id, session_id, generation, user_text, assistant_text, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, deviceID, strings.TrimSpace(turn.SessionID), turn.Generation, userText, assistantText, formatTime(createdAt)); err != nil {
		return fmt.Errorf("append recent turn: %w", err)
	}
	if err := s.pruneRecentTurns(ctx, deviceID); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteMemoryStore) RecentTurns(ctx context.Context, deviceID string, limit int) ([]RecentTurn, error) {
	if s == nil || s.db == nil || limit <= 0 {
		return nil, nil
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT session_id, device_id, generation, user_text, assistant_text, created_at
FROM recent_turns
WHERE device_id = ?
ORDER BY id DESC
LIMIT ?
`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("retrieve recent turns: %w", err)
	}
	defer rows.Close()

	reversed := make([]RecentTurn, 0, limit)
	for rows.Next() {
		var turn RecentTurn
		var createdAt string
		if err := rows.Scan(&turn.SessionID, &turn.DeviceID, &turn.Generation, &turn.UserText, &turn.AssistantText, &createdAt); err != nil {
			return nil, fmt.Errorf("scan recent turn: %w", err)
		}
		turn.CreatedAt = parseTime(createdAt)
		reversed = append(reversed, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent turns: %w", err)
	}
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return reversed, nil
}

func (s *SQLiteMemoryStore) pruneRecentTurns(ctx context.Context, deviceID string) error {
	limit := s.recentTurnLimit
	if limit <= 0 {
		limit = defaultRecentTurnStoreLimit
	}
	_, err := s.db.ExecContext(ctx, `
DELETE FROM recent_turns
WHERE device_id = ?
  AND id NOT IN (
    SELECT id FROM recent_turns WHERE device_id = ? ORDER BY id DESC LIMIT ?
  )
`, deviceID, deviceID, limit)
	if err != nil {
		return fmt.Errorf("prune recent turns: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return fmt.Errorf("enable sqlite wal: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL DEFAULT '',
  device_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL,
  content TEXT NOT NULL,
  importance INTEGER NOT NULL DEFAULT 3,
  confidence REAL NOT NULL DEFAULT 0.8,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT ''
)`); err != nil {
		return fmt.Errorf("migrate memories table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_memories_lookup ON memories(user_id, device_id, importance, updated_at)`); err != nil {
		return fmt.Errorf("migrate memories lookup index: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS recent_turns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  generation INTEGER NOT NULL DEFAULT 0,
  user_text TEXT NOT NULL,
  assistant_text TEXT NOT NULL,
  created_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("migrate recent turns table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_recent_turns_device_id ON recent_turns(device_id, id)`); err != nil {
		return fmt.Errorf("migrate recent turns lookup index: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) touchLastUsed(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET last_used_at = ? WHERE id = ?`, formatTime(at), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func normalizeMemory(memory Memory) Memory {
	now := time.Now()
	if strings.TrimSpace(memory.ID) == "" {
		memory.ID = newMemoryID(now)
	}
	memory.ID = strings.TrimSpace(memory.ID)
	memory.UserID = strings.TrimSpace(memory.UserID)
	memory.DeviceID = strings.TrimSpace(memory.DeviceID)
	memory.SessionID = strings.TrimSpace(memory.SessionID)
	memory.Type = strings.TrimSpace(memory.Type)
	if memory.Type == "" {
		memory.Type = MemoryEpisodic
	}
	memory.Content = strings.TrimSpace(memory.Content)
	if memory.Importance <= 0 {
		memory.Importance = 3
	}
	if memory.Importance > 5 {
		memory.Importance = 5
	}
	if memory.Confidence <= 0 {
		memory.Confidence = 0.8
	}
	if memory.Confidence > 1 {
		memory.Confidence = 1
	}
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	if memory.UpdatedAt.IsZero() {
		memory.UpdatedAt = memory.CreatedAt
	}
	memory.MetadataJSON = strings.TrimSpace(memory.MetadataJSON)
	return memory
}

func validateMemory(memory Memory) error {
	if memory.ID == "" {
		return fmt.Errorf("memory id is required")
	}
	if memory.Content == "" {
		return fmt.Errorf("memory content is required")
	}
	switch memory.Type {
	case MemoryUserProfile, MemoryRelationshipState, MemoryEpisodic, MemoryLorebook:
		return nil
	default:
		return fmt.Errorf("unsupported memory type: %s", memory.Type)
	}
}

func newMemoryID(now time.Time) string {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("mem_%d", now.UnixNano())
	}
	return fmt.Sprintf("mem_%d_%s", now.UnixNano(), hex.EncodeToString(random[:]))
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqliteTimeFormat)
}

func formatNullableTime(t time.Time) string {
	return formatTime(t)
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(sqliteTimeFormat, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
