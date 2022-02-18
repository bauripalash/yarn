// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package session

import (
	"encoding/json"
	"time"
)

// Map  ...
type Map map[string]string

// Session ...
type Session struct {
	store Store

	ID        string    `json:"id"`
	Data      Map       `json:"data"`
	CreatedAt time.Time `json:"created"`
	ExpiresAt time.Time `json:"expires"`
}

func NewSession(store Store) *Session {
	return &Session{store: store}
}

func LoadSession(data []byte, sess *Session) error {
	if err := json.Unmarshal(data, &sess); err != nil {
		return err
	}

	if sess.Data == nil {
		sess.Data = make(Map)
	}

	return nil
}

func (sess *Session) Expired() bool {
	return sess.ExpiresAt.Before(time.Now())
}

func (sess *Session) Set(key, val string) error {
	sess.Data[key] = val
	return sess.store.SyncSession(sess)
}

func (sess *Session) Get(key string) (val string, ok bool) {
	val, ok = sess.Data[key]
	return
}

func (sess *Session) Has(key string) bool {
	_, ok := sess.Data[key]
	return ok
}

func (sess *Session) Del(key string) error {
	delete(sess.Data, key)
	return sess.store.SyncSession(sess)
}

func (sess *Session) Bytes() ([]byte, error) {
	data, err := json.Marshal(sess)
	if err != nil {
		return nil, err
	}
	return data, nil
}
