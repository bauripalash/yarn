// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.mills.io/prologic/bitcask"
	log "github.com/sirupsen/logrus"

	"git.mills.io/yarnsocial/yarn/internal/session"
)

const (
	feedsKeyPrefix    = "/feeds"
	sessionsKeyPrefix = "/sessions"
	usersKeyPrefix    = "/users"
)

// BitcaskStore ...
type BitcaskStore struct {
	db *bitcask.Bitcask
}

func newBitcaskStore(path string) (*BitcaskStore, error) {
	db, err := bitcask.Open(
		path,
		bitcask.WithMaxKeySize(256),
	)
	if err != nil {
		switch {
		case errors.Is(err, &bitcask.ErrBadConfig{}):
			log.WithError(err).Error("error opening database due to bad config")
			if osErr := os.Remove(filepath.Join(path, "config.json")); osErr != nil {
				log.WithError(osErr).Error("error removing bad config")
			}
		case errors.Is(err, &bitcask.ErrBadMetadata{}):
			log.WithError(err).Error("error opening database due to bad metadata")
			if osErr := os.Remove(filepath.Join(path, "meta.json")); osErr != nil {
				log.WithError(osErr).Error("error removing bad metadata")
			}
		}
		return nil, err
	}

	return &BitcaskStore{db: db}, nil
}

func (bs *BitcaskStore) scanKeys(prefix string) (keys [][]byte, err error) {
	err = bs.db.Scan([]byte(prefix), func(key []byte) error {
		keys = append(keys, key)
		return nil
	})

	return
}

// Sync ...
func (bs *BitcaskStore) Sync() error {
	return bs.db.Sync()
}

// Close ...
func (bs *BitcaskStore) Close() error {
	log.Info("syncing store ...")
	if err := bs.db.Sync(); err != nil {
		log.WithError(err).Error("error syncing store")
		return err
	}

	log.Info("closing store ...")
	if err := bs.db.Close(); err != nil {
		log.WithError(err).Error("error closing store")
		return err
	}

	return nil
}

// Merge ...
func (bs *BitcaskStore) Merge() error {
	log.Info("merging store ...")
	if err := bs.db.Merge(); err != nil {
		log.WithError(err).Error("error merging store")
		return err
	}

	return nil
}

func (bs *BitcaskStore) HasFeed(name string) bool {
	key := []byte(fmt.Sprintf("%s/%s", feedsKeyPrefix, name))
	return bs.db.Has(key)
}

func (bs *BitcaskStore) DelFeed(name string) error {
	key := []byte(fmt.Sprintf("%s/%s", feedsKeyPrefix, name))
	return bs.db.Delete(key)
}

func (bs *BitcaskStore) GetFeed(name string) (*Feed, error) {
	key := []byte(fmt.Sprintf("%s/%s", feedsKeyPrefix, name))
	data, err := bs.db.Get(key)
	if err == bitcask.ErrKeyNotFound {
		return nil, ErrFeedNotFound
	}
	return LoadFeed(data)
}

func (bs *BitcaskStore) SetFeed(name string, feed *Feed) error {
	data, err := feed.Bytes()
	if err != nil {
		return err
	}

	key := []byte(fmt.Sprintf("%s/%s", feedsKeyPrefix, name))
	if err := bs.db.Put(key, data); err != nil {
		return err
	}
	return nil
}

func (bs *BitcaskStore) LenFeeds() int64 {
	var count int64

	if err := bs.db.Scan([]byte(feedsKeyPrefix), func(_ []byte) error {
		count++
		return nil
	}); err != nil {
		log.WithError(err).Error("error scanning")
	}

	return count
}

func (bs *BitcaskStore) SearchFeeds(prefix string) []string {
	var keys []string

	if err := bs.db.Scan([]byte(feedsKeyPrefix), func(key []byte) error {
		if strings.HasPrefix(strings.ToLower(string(key)), prefix) {
			keys = append(keys, strings.TrimPrefix(string(key), "/feeds/"))
		}
		return nil
	}); err != nil {
		log.WithError(err).Error("error scanning")
	}

	return keys
}

func (bs *BitcaskStore) GetAllFeeds() ([]*Feed, error) {
	var feeds []*Feed

	keys, err := bs.scanKeys(feedsKeyPrefix)
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		data, err := bs.db.Get(key)
		if err != nil {
			return nil, err
		}

		feed, err := LoadFeed(data)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, feed)
	}

	return feeds, nil
}

func (bs *BitcaskStore) HasUser(username string) bool {
	key := []byte(fmt.Sprintf("%s/%s", usersKeyPrefix, username))
	return bs.db.Has(key)
}

func (bs *BitcaskStore) DelUser(username string) error {
	key := []byte(fmt.Sprintf("%s/%s", usersKeyPrefix, username))
	return bs.db.Delete(key)
}

func (bs *BitcaskStore) GetUser(username string) (*User, error) {
	key := []byte(fmt.Sprintf("%s/%s", usersKeyPrefix, username))
	data, err := bs.db.Get(key)
	if err == bitcask.ErrKeyNotFound {
		return nil, ErrUserNotFound
	}
	return LoadUser(data)
}

func (bs *BitcaskStore) SetUser(username string, user *User) error {
	data, err := user.Bytes()
	if err != nil {
		return err
	}

	key := []byte(fmt.Sprintf("%s/%s", usersKeyPrefix, username))
	if err := bs.db.Put(key, data); err != nil {
		return err
	}
	return nil
}

func (bs *BitcaskStore) LenUsers() int64 {
	var count int64

	if err := bs.db.Scan([]byte(usersKeyPrefix), func(_ []byte) error {
		count++
		return nil
	}); err != nil {
		log.WithError(err).Error("error scanning")
	}

	return count
}

func (bs *BitcaskStore) SearchUsers(prefix string) []string {
	var keys []string

	if err := bs.db.Scan([]byte(usersKeyPrefix), func(key []byte) error {
		if strings.HasPrefix(strings.ToLower(string(key)), prefix) {
			keys = append(keys, strings.TrimPrefix(string(key), "/users/"))
		}
		return nil
	}); err != nil {
		log.WithError(err).Error("error scanning")
	}

	return keys
}

func (bs *BitcaskStore) GetAllUsers() ([]*User, error) {
	var users []*User

	keys, err := bs.scanKeys(usersKeyPrefix)
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		data, err := bs.db.Get(key)
		if err != nil {
			return nil, err
		}

		user, err := LoadUser(data)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func (bs *BitcaskStore) GetSession(sid string) (*session.Session, error) {
	key := []byte(fmt.Sprintf("%s/%s", sessionsKeyPrefix, sid))
	data, err := bs.db.Get(key)
	if err != nil {
		if err == bitcask.ErrKeyNotFound {
			return nil, session.ErrSessionNotFound
		}
		return nil, err
	}
	sess := session.NewSession(bs)
	if err := session.LoadSession(data, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (bs *BitcaskStore) SetSession(sid string, sess *session.Session) error {
	key := []byte(fmt.Sprintf("%s/%s", sessionsKeyPrefix, sid))

	data, err := sess.Bytes()
	if err != nil {
		return err
	}

	return bs.db.Put(key, data)
}

func (bs *BitcaskStore) HasSession(sid string) bool {
	key := []byte(fmt.Sprintf("%s/%s", sessionsKeyPrefix, sid))
	return bs.db.Has(key)
}

func (bs *BitcaskStore) DelSession(sid string) error {
	key := []byte(fmt.Sprintf("%s/%s", sessionsKeyPrefix, sid))
	return bs.db.Delete(key)
}

func (bs *BitcaskStore) SyncSession(sess *session.Session) error {
	// Only persist sessions with a logged in user associated with an account
	// This saves resources as we don't need to keep session keys around for
	// sessions we may never load from the store again.
	if sess.Has("username") {
		return bs.SetSession(sess.ID, sess)
	}
	return nil
}

func (bs *BitcaskStore) LenSessions() int64 {
	var count int64

	if err := bs.db.Scan([]byte(sessionsKeyPrefix), func(_ []byte) error {
		count++
		return nil
	}); err != nil {
		log.WithError(err).Error("error scanning")
	}

	return count
}

func (bs *BitcaskStore) GetAllSessions() ([]*session.Session, error) {
	var sessions []*session.Session

	keys, err := bs.scanKeys(sessionsKeyPrefix)
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		data, err := bs.db.Get(key)
		if err != nil {
			return nil, err
		}

		sess := session.NewSession(bs)
		if err := session.LoadSession(data, sess); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}

	return sessions, nil
}
