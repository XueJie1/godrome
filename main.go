package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"godrome/internal/config"
	"godrome/internal/player"
)

func main() {
	empty := flag.Bool("empty", false, "只打开空窗口，用于基线测量")
	audio := flag.String("audio-output", "", "默认系统输出；自动测试可用 null")
	configFile := flag.String("config", "", "配置文件路径")
	quitAfter := flag.Duration("quit-after", 0, "自动退出时间，用于受控测量")
	autoplay := flag.Bool("autoplay", false, "自动播放命令行提供的本地音频")
	metrics := flag.String("metrics", "", "写入 Go 堆和 goroutine 测量 CSV（不主动 GC）")
	flag.Parse()
	stopMetrics, err := startMetrics(*metrics)
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法创建测量文件")
		os.Exit(1)
	}
	defer stopMetrics()
	path := *configFile
	if path == "" {
		var err error
		path, err = config.Path()
		if err != nil {
			fmt.Fprintln(os.Stderr, "无法定位配置目录")
			os.Exit(1)
		}
	}
	settings, err := config.Load(path)
	engine := player.New(player.Options{AudioOutput: *audio})
	a := newApp(settings, path, engine)
	if err != nil {
		a.errorText = "读取配置失败，已使用默认设置"
	}
	if len(flag.Args()) > 0 {
		a.local(flag.Args())
	}
	a.setAppearance = mygo.Theme.SetSource
	if a.appearance == "light" || a.appearance == "dark" {
		mygo.Theme.SetSource(appearanceSource(a.appearance, false))
	}
	mygo.App.WhenReady(func() {
		view := a.view
		if *empty {
			view = func(c *ui.Context) { ui.Column(c).Fill() }
		}
		win := mygo.NewWindow(mygo.WindowOptions{Title: "Godrome", Width: settings.Width, Height: settings.Height, MinWidth: 800, MinHeight: 560, Content: ui.View(view)})
		a.post = win.Update
		a.listen()
		// The desktop media widget can raise or close the window too.
		a.startMPRIS(func() { win.Show(); win.Focus() }, func() { win.Close() })
		a.control(engine.SetVolume(settings.Volume))
		a.applyEqualizer()
		win.OnHide(func() { a.control(engine.SetVisible(false)) })
		win.OnMinimize(func() { a.control(engine.SetVisible(false)) })
		win.OnShow(func() { a.control(engine.SetVisible(true)) })
		win.OnRestore(func() { a.control(engine.SetVisible(true)) })
		win.OnClose(func(*mygo.CloseEvent) {
			a.settings.Width, a.settings.Height = win.Size()
			a.settings.Volume = a.volume
			a.settings.SidebarWidth = a.sidebarWidth
			a.settings.QueueWidth = a.queueWidth
			if err := config.Save(path, a.settings); err != nil {
				fmt.Fprintln(os.Stderr, "保存配置失败")
			}
			a.close()
		})
		win.OnClosed(func() { mygo.App.Quit() })
		if *autoplay && len(a.songs) > 0 {
			a.playSongs(0)
		}
		if *quitAfter > 0 {
			go func() {
				select {
				case <-a.ctx.Done():
					return
				case <-time.After(*quitAfter):
					win.Update(func() { win.Close() })
				}
			}()
		}
	})
	if err := mygo.App.Run(); err != nil {
		a.close()
		fmt.Fprintln(os.Stderr, "Godrome 无法启动：", err)
		os.Exit(1)
	}
	a.close()
}
