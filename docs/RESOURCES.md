# 零基础 Go 学习资料

核对日期：2026-10-21。以下选择和顺序是针对“从零理解这个项目”的建议，不需要同时通读所有资料。

## 先用中文小例子学语法

[Go by Example 中文版](https://gobyexample-cn.github.io/) 每个主题都有带说明的小程序，适合读一段、运行一段。先从 [Hello World](https://gobyexample-cn.github.io/hello-world) 开始。

建议先看这些主题：变量 → for → if/else → switch → [函数](https://gobyexample-cn.github.io/functions) → 多返回值 → [切片](https://gobyexample-cn.github.io/slices) → 结构体 → 方法 → 接口 → 错误处理。

每次只做三件事：自己敲一遍，猜输出，再改一个值或分支看结果。暂时跳过并发相关章节，先掌握顺序执行。

## 用官方教程确认如何运行程序

[Go 官方：Get started with Go](https://go.dev/doc/tutorial/getting-started) 介绍创建第一个程序、运行命令和模块。是英文，可以配合翻译阅读。

它注明最好已有一点编程经验；如果“函数、参数、返回值”还陌生，先做上面的几个中文例子，再回来看。教程中的外部包练习放在独立练习目录，本项目仍保持标准库依赖。

## 用互动练习巩固

[A Tour of Go](https://go.dev/tour/) 是官方的互动入门，包含练习，可以在网页中修改并运行代码；官方[学习页](https://go.dev/learn/)也介绍了它的用途。

建议学完一组语法后，用其中的练习检查自己是否能独立写出来。不必为了“完成教程”一次跳到并发等后续主题。这个入口是英文。

## 遇到概念卡住，再查详细解释

[《Go 语言圣经》中文版](https://gopl-zh.github.io/) 按章节解释语言机制。建议先查第 2 章程序结构、第 4 章复合数据类型、第 5 章函数，随后再读方法和接口。

我建议把它作为第二遍补充阅读。刚起步时，先能写出小程序，再回头理解细节。书中涉及的安装和工具用法，以当前官方文档为准。

## 和当前项目一起学

| 正在读的项目文件 | 需要的知识 | 建议资料 |
| --- | --- | --- |
| `cmd/miao/main.go` | 变量、函数、循环、条件、错误、回调 | Go by Example 对应基础主题，然后读项目[代码导读](READING_GUIDE.md)第一步 |
| `internal/agent/agent.go` | 结构体、指针、切片、方法、接口 | 先做[切片示例](https://gobyexample-cn.github.io/slices)，再手画两轮对话的消息列表 |
| `internal/llm/client.go` | JSON、HTTP、defer、逐行读取 | [JSON 示例](https://gobyexample-cn.github.io/json)、[HTTP 客户端示例](https://gobyexample-cn.github.io/http-clients) |
| `*_test.go` | 假客户端、输入、期望输出、断言 | 先读项目 Agent 测试，再查 Go by Example 的单元测试主题 |

当前项目的 SSE 解析可以最后再读：先理解普通 HTTP 请求、响应体和按行读取，再跟着注释看 `data:` 如何变成回答文字。

## 现在就可以开始的三个练习

1. 自己写并运行 Hello World，改掉输出文字，解释 `package`、`import`、`main` 各做什么。
2. 修改本项目 `/help` 的一句文字，运行 `make run`，确认你找对了代码位置。
3. 能看懂 `Agent.Send` 后，自己尝试添加 `/count`，显示完整问答轮数；先说明系统消息是否计数，再写测试。

完成标准：合上资料后仍能解释代码，并能预测改动的结果。下一阶段再按[学习路线](ROADMAP.md)引入工具调用。
