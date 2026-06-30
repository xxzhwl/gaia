// Package workflow 提供可嵌入的 Gaia 工作流组件。
//
// 组件负责流程定义、流程实例、节点执行、流程变量、审计事件、持久化和外部任务调度。
// 业务服务可以直接嵌入该组件使用，gaia-workflow 则是在同一套核心运行时外面包了一层
// HTTP 服务外壳。
package workflow
