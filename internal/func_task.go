// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import "fmt"

type FuncTask struct {
	*BaseTask

	f func() error
}

func NewFuncTask(f func() error) *FuncTask {
	return &FuncTask{
		BaseTask: NewBaseTask(),

		f: f,
	}
}

func (t *FuncTask) String() string { return fmt.Sprintf("%T: %s", t, t.ID()) }
func (t *FuncTask) Run() error {
	defer t.Done()
	t.SetState(TaskStateRunning)

	return t.f()
}
