// Package bootstrap 负责控制面的「开箱可用」初始化。
//
// 单用户自托管没有可用的账号创建界面：没有这一步，新部署必须先手抄
// enrollment token 再 curl 一个接口才能登录。策略是空库时建一个默认管理员，
// 并把这个决定集中在一个包里，让入口与测试共用同一份常量。
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// DefaultAdminEmail 是自动引导创建的管理员登录名。
	//
	// 它刻意不带 @：users 表的唯一键就是 email 字段，服务端只要求非空，
	// 账号名不必长得像邮箱。
	DefaultAdminEmail = "admin"
	// DefaultAdminPassword 是自动引导的默认密码。启动日志与运维文档都会
	// 明写它，并要求部署后立即修改——隐藏默认值只会让人改不了。
	DefaultAdminPassword = "admin123"
)

// EnsureAdmin 在库中没有任何账号时创建默认管理员。
//
// 已有账号时什么都不做：老部署的管理员密码不能被一次升级覆盖。多实例同时
// 启动可能都看到空库，后到的那个会撞邮箱唯一索引——这不算失败，重新查一次
// 确认账号已存在即可。
func EnsureAdmin(ctx context.Context, st *store.Store) (store.User, bool, error) {
	if st == nil {
		return store.User{}, false, errors.New("bootstrap: store is required")
	}
	count, err := st.CountUsers(ctx)
	if err != nil {
		return store.User{}, false, fmt.Errorf("bootstrap: count users: %w", err)
	}
	if count > 0 {
		return store.User{}, false, nil
	}

	hash, err := authn.HashPassword(DefaultAdminPassword)
	if err != nil {
		return store.User{}, false, fmt.Errorf("bootstrap: hash default password: %w", err)
	}
	user, err := st.CreateUser(ctx, store.User{
		Email:        DefaultAdminEmail,
		PasswordHash: hash,
		Role:         "admin",
		Status:       "active",
	})
	if err != nil {
		// 并发启动的另一实例已经建好了：以库里那条为准。
		if existing, found, lookupErr := st.UserByEmail(ctx, DefaultAdminEmail); lookupErr == nil && found {
			return existing, false, nil
		}
		return store.User{}, false, fmt.Errorf("bootstrap: create admin: %w", err)
	}
	return user, true, nil
}
