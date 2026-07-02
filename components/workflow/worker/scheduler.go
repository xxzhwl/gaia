package worker

import (
	coretask "github.com/xxzhwl/gaia/components/asynctask"
	"github.com/xxzhwl/gaia"
)

// registeredThemes 收集当前已注册方法涉及的全部 theme（去重）。
func (w *Worker) registeredThemes() []string {
	seen := map[string]struct{}{}
	themes := make([]string, 0)
	for _, binding := range w.catalog.Bindings() {
		if binding.Theme == "" {
			continue
		}
		if _, ok := seen[binding.Theme]; ok {
			continue
		}
		seen[binding.Theme] = struct{}{}
		themes = append(themes, binding.Theme)
	}
	return themes
}

// ensureScheduler 按 theme 懒加载并启动 asynctask 调度器（核心执行层），
// 注入本 worker 的 PreHandler/PostHandler。同一 theme 只会启动一个调度器。
func (w *Worker) ensureScheduler(theme string) *coretask.Scheduler {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s, ok := w.schedulers[theme]; ok {
		return s
	}

	opts := append([]coretask.SchedulerOption{
		coretask.WithPreHandler(w.preHandler()),
		coretask.WithPostHandler(w.postHandler()),
	}, w.schedulerOpts...)

	scheduler := coretask.NewScheduler(theme, opts...)
	if w.bootstrap {
		if err := scheduler.Bootstrap(w.baseCtx); err != nil {
			gaia.ErrorF("workflow worker bootstrap asynctask tables for theme %s failed: %v", theme, err)
		}
	}
	if !scheduler.IsRunning() {
		coretask.StartScheduler(w.baseCtx, theme, opts...)
	}
	w.schedulers[theme] = scheduler
	return scheduler
}
