// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"context"

	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
)

func GetRequestUserId(thread *starlark.Thread) string {
	ctxVal := thread.Local(types.TL_CONTEXT)
	if ctxVal == nil {
		return ""
	}

	ctx, ok := ctxVal.(context.Context)
	if !ok {
		return ""
	}

	return GetContextUserId(ctx)
}

func GetContextUserId(ctx context.Context) string {
	userId := ctx.Value(types.USER_ID)
	if userId == nil {
		return ""
	}
	strValue, ok := userId.(string)
	if !ok {
		return ""
	}
	return strValue
}
