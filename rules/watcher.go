package rules

import (
	"log"
	"time"

	"github.com/fsnotify/fsnotify"
)

func init() { go watchRules() }

func watchRules() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[watch] %v", err)
		return
	}
	defer w.Close()

	_ = w.Add(txtFile) // 监听 rules.txt
	for {
		select {
		case ev := <-w.Events:
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				log.Printf("[watch] %s changed, reloading", txtFile)
				// 只需把 mtime 置零，下次 Match 会强制重新解析
				mt = time.Time{}
			}
		case err := <-w.Errors:
			log.Printf("[watch] %v", err)
		}
	}
}
