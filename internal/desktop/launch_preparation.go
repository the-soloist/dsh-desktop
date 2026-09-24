package desktop

import (
	"context"
	"errors"
	"fmt"

	"github.com/the-soloist/dsh-desktop/internal/backend"
)

type externalDSHChoice int

const (
	externalDSHCancel externalDSHChoice = iota
	externalDSHKill
	externalDSHOtherPort
)

var errStartupCancelled = errors.New("已取消启动")

// launchPreparation contains startup policy, without a dependency on native UI.
// Both initial startup and restart discover processes before selecting a port.
type launchPreparation struct {
	discover  func(context.Context) ([]backend.DSHProcess, error)
	reuse     func(context.Context) bool
	choose    func(context.Context, []backend.DSHProcess) (externalDSHChoice, error)
	stop      func(context.Context, []backend.DSHProcess) error
	available func(int) bool
}

type launchPlan struct {
	port  int
	reuse bool
}

func newLaunchPreparation(supervisor *backend.Supervisor, choose func(context.Context, []backend.DSHProcess) (externalDSHChoice, error)) launchPreparation {
	return launchPreparation{
		discover:  backend.FindDSHProcesses,
		reuse:     func(ctx context.Context) bool { return reusableDSH(ctx, supervisor) },
		choose:    choose,
		stop:      backend.StopDSHProcesses,
		available: backend.PortAvailable,
	}
}

func (preparation launchPreparation) prepare(ctx context.Context, preferredPort int) (launchPlan, error) {
	if err := ctx.Err(); err != nil {
		return launchPlan{}, err
	}
	processes, err := preparation.discover(ctx)
	if err != nil {
		return launchPlan{}, fmt.Errorf("检查已有 DSH 进程失败：%w", err)
	}
	if preparation.reuse != nil && preparation.reuse(ctx) {
		return launchPlan{port: preferredPort, reuse: true}, ctx.Err()
	}
	if len(processes) > 0 {
		choice, err := preparation.choose(ctx, processes)
		if err != nil {
			return launchPlan{}, err
		}
		if err := ctx.Err(); err != nil {
			return launchPlan{}, err
		}
		switch choice {
		case externalDSHKill:
			if err := preparation.stop(ctx, processes); err != nil {
				return launchPlan{}, fmt.Errorf("结束已有 DSH 进程失败：%w", err)
			}
		case externalDSHOtherPort:
		default:
			return launchPlan{}, errStartupCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return launchPlan{}, err
	}
	port, err := backend.NextAvailablePort(preferredPort, preparation.available)
	if err != nil {
		return launchPlan{}, err
	}
	return launchPlan{port: port}, nil
}
