package sip

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestObserver(t *testing.T) {
	s := NewObserver()

	// 注册观察者 1
	go func() {
		s.Register("1", 6*time.Second, func(did string, args ...string) bool {
			fmt.Printf("Observer 1 triggered: %s %s\n", did, strings.Join(args, " "))
			return true
		})
	}()

	// 注册观察者 2
	go func() {
		s.Register("2", 6*time.Second, func(did string, args ...string) bool {
			fmt.Printf("Observer 2 triggered: %s %s\n", did, strings.Join(args, " "))
			return true
		})
	}()

	// 等待注册完成
	time.Sleep(1 * time.Second)

	// 通知观察者
	s.Notify("1")
	s.Notify("2")

	// 等待通知处理完成
	time.Sleep(1 * time.Second)

	// 检查剩余观察者数量
	var i int
	s.data.Range(func(key string, value ObserverHandler) bool {
		fmt.Printf("Remaining key: %s\n", key)
		i++
		return true
	})
	fmt.Println("Remaining observers:", i)
}
