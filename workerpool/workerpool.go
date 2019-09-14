package workerpool

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/xxjwxc/public/mylog"
)

//New 注册工作池，并设置最大并发数
//new workpool and set the max number of concurrencies
func New(max int) *WorkerPool {
	if max < 1 {
		max = 1
	}

	return &WorkerPool{
		maxWorkersCount: max,
		task:            make(chan TaskHandler, max),
		errChan:         make(chan error, 1),
	}
}

//SetTimeout 设置超时时间
func (p *WorkerPool) SetTimeout(timeout time.Duration) {
	p.timeout = timeout
}

//SingleCall 单程执行(排他)
// func (p *WorkerPool) SingleCall(fn TaskHandler) {
// 	p.Mutex.Lock()
// 	fn()
// 	p.Mutex.Unlock()
// }

//Do 添加到工作池，并立即返回
func (p *WorkerPool) Do(fn TaskHandler) {
	p.start.Do(func() { //once
		p.wg.Add(p.maxWorkersCount)
		go p.loop()
	})

	if atomic.LoadInt32(&p.closed) == 1 {
		// 已关闭
		return
	}
	p.task <- fn
}

//DoWait 添加到工作池，并等待执行完成之后再返回
func (p *WorkerPool) DoWait(task TaskHandler) {
	p.start.Do(func() { //once
		p.wg.Add(p.maxWorkersCount)
		go p.loop()
	})

	if atomic.LoadInt32(&p.closed) == 1 { // 已关闭
		return
	}

	doneChan := make(chan struct{})
	p.task <- func() error {
		err := task()
		close(doneChan)
		return err
	}
	<-doneChan
}

func (p *WorkerPool) loop() {
	// 启动n个worker
	for i := 0; i < p.maxWorkersCount; i++ {
		go func() {
			defer p.wg.Done()
			// worker 开始干活
			for wt := range p.task {
				if wt == nil || atomic.LoadInt32(&p.closed) == 1 { //有err 立即返回
					continue //需要先消费完了之后再返回，
				}

				closed := make(chan struct{}, 1)
				// 有设置超时,优先task 的超时
				if p.timeout > 0 {
					ct, cancel := context.WithTimeout(context.Background(), p.timeout)
					go func() {
						select {
						case <-ct.Done():
							p.errChan <- ct.Err()
							//if atomic.LoadInt32(&p.closed) != 1 {
							mylog.Error(ct.Err())
							atomic.StoreInt32(&p.closed, 1)
							cancel()
						case <-closed:
						}
					}()
				}

				err := wt() //真正执行的点
				close(closed)
				if err != nil {
					select {
					case p.errChan <- err:
						//if atomic.LoadInt32(&p.closed) != 1 {
						mylog.Error(err)
						atomic.StoreInt32(&p.closed, 1)
					default:
					}
				}
			}
		}()
	}
}

//Wait 等待工作线程执行结束
func (p *WorkerPool) Wait() error {
	close(p.task)
	p.wg.Wait() //等待结束
	select {
	case err := <-p.errChan:
		return err
	default:
		return nil
	}
}

//IsDone 判断是否完成 (非阻塞)
func (p *WorkerPool) IsDone() bool {
	if p == nil || p.task == nil {
		return true
	}

	return len(p.task) == 0
}