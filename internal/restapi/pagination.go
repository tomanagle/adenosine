package restapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/google/uuid"
)

const (
	defaultPageLimit = 30
	cursorVersion    = 1
)

var errInvalidCursor = errors.New("invalid pagination cursor")

type pageCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Key     string `json:"k"`
}

func paginationInputs(limit *generated.Limit, cursor *generated.Cursor) (*int, *string) {
	var requestedLimit *int
	if limit != nil {
		value := int(*limit)
		requestedLimit = &value
	}
	var encodedCursor *string
	if cursor != nil {
		value := string(*cursor)
		encodedCursor = &value
	}
	return requestedLimit, encodedCursor
}

func collectionParameters(limit *generated.Limit, cursor *generated.Cursor) (int, string) {
	value := defaultPageLimit
	if limit != nil {
		value = int(*limit)
	}
	if cursor == nil {
		return value, ""
	}
	return value, string(*cursor)
}

func paginate[T any](values []T, requestedLimit *int, encodedCursor *string, scope string, key func(T) string) ([]T, *string, error) {
	limit := defaultPageLimit
	if requestedLimit != nil {
		limit = *requestedLimit
	}
	if limit < 1 || limit > 100 {
		return nil, nil, fmt.Errorf("%w: limit must be between 1 and 100", errInvalidCursor)
	}

	start := 0
	if encodedCursor != nil && *encodedCursor != "" {
		cursor, err := decodePageCursor(*encodedCursor, scope)
		if err != nil {
			return nil, nil, err
		}
		found := false
		for index, value := range values {
			if key(value) == cursor.Key {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("%w: cursor no longer belongs to this collection", errInvalidCursor)
		}
	}

	if start >= len(values) {
		return []T{}, nil, nil
	}
	end := min(start+limit, len(values))
	items := append([]T(nil), values[start:end]...)
	if items == nil {
		items = []T{}
	}
	if end == len(values) {
		return items, nil, nil
	}
	next, err := encodePageCursor(pageCursor{Version: cursorVersion, Scope: scope, Key: key(values[end-1])})
	if err != nil {
		return nil, nil, err
	}
	return items, &next, nil
}

func encodePageCursor(cursor pageCursor) (string, error) {
	value, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode pagination cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func decodePageCursor(encoded, scope string) (pageCursor, error) {
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return pageCursor{}, fmt.Errorf("%w: decode: %v", errInvalidCursor, err)
	}
	var cursor pageCursor
	if err := json.Unmarshal(value, &cursor); err != nil {
		return pageCursor{}, fmt.Errorf("%w: decode: %v", errInvalidCursor, err)
	}
	if cursor.Version != cursorVersion || cursor.Scope != scope || cursor.Key == "" {
		return pageCursor{}, errInvalidCursor
	}
	return cursor, nil
}

func decodeCollectionCursor(encoded, scope string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	cursor, err := decodePageCursor(encoded, scope)
	if err != nil {
		return "", err
	}
	return cursor.Key, nil
}

func decodeUUIDCollectionCursor(encoded, scope string) (*uuid.UUID, error) {
	key, err := decodeCollectionCursor(encoded, scope)
	if err != nil || key == "" {
		return nil, err
	}
	id, err := uuid.Parse(key)
	if err != nil {
		return nil, errInvalidCursor
	}
	return &id, nil
}

func encodeCollectionCursor(scope, key string) (*string, error) {
	if key == "" {
		return nil, nil
	}
	encoded, err := encodePageCursor(pageCursor{Version: cursorVersion, Scope: scope, Key: key})
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}
