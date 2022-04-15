// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"fmt"
	"io/ioutil"

	"github.com/naoina/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

type Translator struct {
	Bundle *i18n.Bundle
}

func NewTranslator() (*Translator, error) {
	// Default lang is english but bundle will store multiple lang!
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// This is where we need to load the "Lang" context... but how???
	lang := "en"

	langfile, err := ioutil.ReadFile(fmt.Sprintf("./data/langs/active.%s.toml", lang))
	if err != nil {
		return nil, fmt.Errorf("error loading locale: %w", err)
	}

	bundle.MustParseMessageFileBytes(langfile, fmt.Sprintf("active.%s.toml", lang))

	return &Translator{
		Bundle: bundle,
	}, nil
}

// Translate 翻译
func (t *Translator) Translate(ctx *Context, msgID string, data ...interface{}) string {
	localizer := i18n.NewLocalizer(t.Bundle, ctx.Lang, ctx.AcceptLangs)

	conf := i18n.LocalizeConfig{
		MessageID: msgID,
	}
	if len(data) > 0 {
		conf.TemplateData = data[0]
	}

	return localizer.MustLocalize(&conf)
}
