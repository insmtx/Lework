package dm

// WorkerTaskConsumer 返回 worker 任务消费者的持久化消费者名称。
// 同一 topic 的多个实例共享同一个 durable consumer，由 NATS 做负载均衡。
func WorkerTaskConsumer() string {
	return "worker-task-consumer"
}

// SessionTitleConsumer 构造会话标题处理器的持久化消费者名称。
func SessionTitleConsumer() string {
	return "session-title-handler"
}

// SessionCompletedConsumer 构造会话完成处理器的持久化消费者名称。
func SessionCompletedConsumer() string {
	return "session-completed-handler"
}
