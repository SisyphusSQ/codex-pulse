package cursorprovider

import (
	"context"
	"sync"
)

type GlobalRefreshReport struct {
	Local     OnlineRefreshClass
	Dashboard OnlineRefreshClass
	GrokBot   OnlineRefreshClass
}

func (service *QueryService) RefreshForGlobal(ctx context.Context, interactive bool) GlobalRefreshReport {
	if service == nil || service.collector == nil || ctx == nil {
		return GlobalRefreshReport{
			Local:     OnlineRefreshClass{Err: ErrCollector},
			Dashboard: OnlineRefreshClass{Err: ErrCollector},
			GrokBot:   OnlineRefreshClass{Err: ErrCollector},
		}
	}
	report := GlobalRefreshReport{}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		performed, err := service.collector.RefreshIfDue(ctx)
		report.Local = OnlineRefreshClass{Attempted: performed, Err: err}
	}()
	go func() {
		defer wg.Done()
		if service.dashboard == nil {
			report.Dashboard = OnlineRefreshClass{Err: ErrCollector}
			return
		}
		report.Dashboard = refreshCursorOnline(ctx, service.dashboard, interactive)
	}()
	go func() {
		defer wg.Done()
		service.refreshMu.Lock()
		grokBot := service.grokBot
		service.refreshMu.Unlock()
		if grokBot == nil {
			report.GrokBot = OnlineRefreshClass{Err: ErrCollector}
			return
		}
		report.GrokBot = refreshCursorOnline(ctx, grokBot, interactive)
	}()
	wg.Wait()
	if (report.Local.Attempted && report.Local.Err == nil) ||
		(report.Dashboard.Attempted && report.Dashboard.Err == nil) ||
		(report.GrokBot.Attempted && report.GrokBot.Err == nil) {
		service.invalidateSnapshot()
	}
	return report
}

func refreshCursorOnline(ctx context.Context, refresher Refresher, interactive bool) OnlineRefreshClass {
	if classified, ok := refresher.(classifiedOnlineRefresher); ok {
		return classified.RefreshClassified(ctx, interactive)
	}
	if conditional, ok := refresher.(conditionalRefresher); ok && !interactive {
		performed, err := conditional.RefreshIfDue(ctx)
		return OnlineRefreshClass{Attempted: performed, Remote: performed || err != nil, Err: err}
	}
	err := refresher.Refresh(ctx)
	return classifyCursorOnlineError(err)
}
