package main

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// backendService 是组合进程中一个长期运行的后台服务：run 阻塞到 ctx 结束或
// 自身失败，close 在进程退出时释放资源。
type backendService struct {
	name  string
	run   func(context.Context) error
	close func() error
}

// runServices 并发托管所有服务，直到某个服务失败或 ctx 被取消。
//
// 任一服务返回错误会取消同组服务；ctx 仍然有效时返回 nil 的服务视为意外
// 退出并转换成错误，避免进程“静默少了一个服务”。所有服务结束后按启动的
// 逆序关闭，close 错误与主错误用 errors.Join 合并：主错误排在前面，既不
// 掩盖真正的退出原因，也不丢弃关闭阶段的失败。
func runServices(ctx context.Context, services []backendService) error {
	for _, svc := range services {
		if svc.run == nil {
			return fmt.Errorf("service %s has no run function", svc.name)
		}
	}

	group, groupCtx := errgroup.WithContext(ctx)
	for _, svc := range services {
		svc := svc
		group.Go(func() error {
			err := svc.run(groupCtx)
			if err == nil && groupCtx.Err() == nil {
				return fmt.Errorf("%s exited unexpectedly", svc.name)
			}
			return err
		})
	}

	runErr := group.Wait()

	closeErrs := make([]error, 0, len(services))
	for i := len(services) - 1; i >= 0; i-- {
		svc := services[i]
		if svc.close == nil {
			continue
		}
		if err := svc.close(); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("close %s: %w", svc.name, err))
		}
	}

	return errors.Join(append([]error{runErr}, closeErrs...)...)
}
