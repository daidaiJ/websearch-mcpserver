package quality

import "websearch/pkg/search/core"

// qualityCase is a fixed ranking fixture: the same query and candidate set is
// replayed on every run, so a weight or threshold change shows up as a metric
// delta rather than an opinion.
//
// Candidates are intentionally synthetic and small. This harness measures the
// ranking pipeline's regression behaviour, not live retrieval quality; the
// roadmap documents the same limitation for the reference implementation.
type qualityCase struct {
	Name         string
	Query        string
	Expected     []core.SearchResult
	Distractors  []core.SearchResult
	MinHits      int // expected-domain results that must appear in the top 8
	MaxFirstRank int // first expected-domain result must rank at or above this
}

func r(title, url, content string) core.SearchResult {
	return core.SearchResult{Title: title, Url: url, Content: content}
}

func fixtures() []qualityCase {
	return []qualityCase{
		{
			Name:  "go-generics-en",
			Query: "Go generics performance",
			Expected: []core.SearchResult{
				r("Go generics performance in practice", "https://go.dev/blog/generics-performance", "Benchmarks and compiler tradeoffs for Go generics performance."),
				r("Go generics performance discussion", "https://github.com/golang/go/issues/12345", "Issue thread measuring Go generics performance across releases."),
			},
			Distractors: []core.SearchResult{
				r("Java generics tutorial", "https://docs.oracle.com/javase/tutorial/java/generics/", "Java generics and type erasure."),
				r("C++ templates reference", "https://en.cppreference.com/w/cpp/language/templates", "C++ template metaprogramming."),
				r("Python typing documentation", "https://docs.python.org/3/library/typing.html", "Python type hints and generics."),
				r("Web performance metrics", "https://web.dev/vitals/", "Core Web Vitals and page performance."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "react-vue-en",
			Query: "React vs Vue ecosystem comparison",
			Expected: []core.SearchResult{
				r("React ecosystem overview", "https://react.dev/learn", "React ecosystem, tooling and comparison notes."),
				r("Vue ecosystem guide", "https://vuejs.org/guide/introduction.html", "Vue ecosystem comparison with React."),
			},
			Distractors: []core.SearchResult{
				r("Angular components", "https://angular.io/guide/component-overview", "Angular component architecture."),
				r("Svelte tutorial", "https://svelte.dev/tutorial", "Svelte reactivity tutorial."),
				r("Webpack configuration", "https://webpack.js.org/configuration/", "Webpack bundler configuration."),
				r("npm package registry", "https://www.npmjs.com/", "JavaScript package registry."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "postgres-index-en",
			Query: "PostgreSQL index tuning",
			Expected: []core.SearchResult{
				r("PostgreSQL index tuning guide", "https://www.postgresql.org/docs/current/indexes.html", "PostgreSQL index types and tuning guidance."),
				r("PostgreSQL index tuning in production", "https://pganalyze.com/blog/postgres-index-tuning", "Production PostgreSQL index tuning notes."),
			},
			Distractors: []core.SearchResult{
				r("MySQL indexes", "https://dev.mysql.com/doc/refman/8.0/en/mysql-indexes.html", "MySQL index documentation."),
				r("SQLite query planner", "https://www.sqlite.org/optoverview.html", "SQLite query planner overview."),
				r("MongoDB indexes", "https://www.mongodb.com/docs/manual/indexes/", "MongoDB index types."),
				r("Redis persistence", "https://redis.io/docs/management/persistence/", "Redis persistence modes."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "rust-async-en",
			Query: "Rust async runtime",
			Expected: []core.SearchResult{
				r("Rust async programming", "https://doc.rust-lang.org/book/ch17-00-async-await.html", "Rust async runtime and futures."),
				r("Tokio async runtime tutorial", "https://tokio.rs/tokio/tutorial", "Tokio async runtime for Rust."),
			},
			Distractors: []core.SearchResult{
				r("Go goroutines", "https://go.dev/tour/concurrency/1", "Go goroutines and channels."),
				r("Python asyncio", "https://docs.python.org/3/library/asyncio.html", "Python asyncio event loop."),
				r("Node.js event loop", "https://nodejs.org/en/learn/asynchronous-work/event-loop-timers-and-nexttick", "Node.js event loop."),
				r("Java threads", "https://docs.oracle.com/javase/tutorial/essential/concurrency/", "Java concurrency tutorial."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "k8s-eviction-en",
			Query: "Kubernetes pod eviction",
			Expected: []core.SearchResult{
				r("Kubernetes node-pressure eviction", "https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/", "Kubernetes pod eviction under node pressure."),
				r("Kubernetes eviction API issue", "https://github.com/kubernetes/kubernetes/issues/45678", "Kubernetes pod eviction API behaviour."),
			},
			Distractors: []core.SearchResult{
				r("Docker container lifecycle", "https://docs.docker.com/engine/reference/commandline/container/", "Docker container lifecycle commands."),
				r("AWS ECS task placement", "https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-placement.html", "ECS task placement."),
				r("Nomad job scheduling", "https://developer.hashicorp.com/nomad/docs/concepts/scheduling", "Nomad scheduling concepts."),
				r("OpenShift projects", "https://docs.openshift.com/container-platform/latest/applications/projects/working-with-projects.html", "OpenShift projects."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "python-asyncio-timeout-en",
			Query: "Python asyncio timeout",
			Expected: []core.SearchResult{
				r("Python asyncio timeouts", "https://docs.python.org/3/library/asyncio-task.html#timeouts", "Python asyncio timeout handling and wait_for."),
				r("Python asyncio timeout patterns", "https://realpython.com/async-io-python/", "Async IO timeout patterns in Python."),
			},
			Distractors: []core.SearchResult{
				r("Node.js timers", "https://nodejs.org/api/timers.html", "Node.js timer functions."),
				r("Go context cancellation", "https://pkg.go.dev/context", "Go context deadlines and cancellation."),
				r("Rust Tokio time", "https://docs.rs/tokio/latest/tokio/time/", "Tokio time utilities."),
				r("Java futures", "https://docs.oracle.com/javase/8/docs/api/java/util/concurrent/Future.html", "Java Future API."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "docker-multistage-en",
			Query: "Docker multi-stage build",
			Expected: []core.SearchResult{
				r("Docker multi-stage builds", "https://docs.docker.com/build/building/multi-stage/", "Docker multi-stage build documentation."),
				r("Docker multi-stage build examples", "https://github.com/docker/docs", "Docker multi-stage build examples."),
			},
			Distractors: []core.SearchResult{
				r("Podman build", "https://podman.io/docs", "Podman container build documentation."),
				r("Buildah tutorials", "https://buildah.io/", "Buildah image build tutorials."),
				r("Kaniko image builds", "https://github.com/GoogleContainerTools/kaniko", "Kaniko in-cluster image builds."),
				r("containerd runtime", "https://containerd.io/docs/", "containerd runtime documentation."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "ts-satisfies-en",
			Query: "TypeScript satisfies operator",
			Expected: []core.SearchResult{
				r("TypeScript satisfies operator", "https://www.typescriptlang.org/docs/handbook/2/satisfies-operator.html", "The TypeScript satisfies operator reference."),
				r("Announcing TypeScript 4.9 satisfies", "https://devblogs.microsoft.com/typescript/announcing-typescript-4-9/", "TypeScript 4.9 introduced the satisfies operator."),
			},
			Distractors: []core.SearchResult{
				r("Babel configuration", "https://babeljs.io/docs/configuration", "Babel configuration reference."),
				r("Flow type annotations", "https://flow.org/en/docs/types/", "Flow type annotations."),
				r("JSDoc reference", "https://jsdoc.app/", "JSDoc tags and annotations."),
				r("ESLint rules", "https://eslint.org/docs/latest/rules/", "ESLint rule reference."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "sqlite-wal-en",
			Query: "SQLite WAL mode",
			Expected: []core.SearchResult{
				r("SQLite write-ahead logging", "https://www.sqlite.org/wal.html", "SQLite WAL mode documentation."),
				r("SQLite WAL implementation", "https://github.com/sqlite/sqlite", "SQLite WAL mode implementation."),
			},
			Distractors: []core.SearchResult{
				r("PostgreSQL write-ahead log", "https://www.postgresql.org/docs/current/wal-intro.html", "PostgreSQL WAL internals."),
				r("MySQL InnoDB redo log", "https://dev.mysql.com/doc/refman/8.0/en/innodb-redo-log.html", "InnoDB redo log."),
				r("RocksDB write-ahead log", "https://github.com/facebook/rocksdb/wiki/Write-Ahead-Log", "RocksDB WAL."),
				r("LevelDB implementation", "https://github.com/google/leveldb", "LevelDB storage engine."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "mcp-spec-en",
			Query: "MCP protocol specification",
			Expected: []core.SearchResult{
				r("Model Context Protocol specification", "https://modelcontextprotocol.io/specification", "MCP protocol specification and lifecycle."),
				r("MCP specification repository", "https://github.com/modelcontextprotocol/specification", "Model Context Protocol specification source."),
			},
			Distractors: []core.SearchResult{
				r("OpenAI API reference", "https://platform.openai.com/docs/api-reference", "OpenAI API reference."),
				r("Anthropic API docs", "https://docs.anthropic.com/", "Anthropic API documentation."),
				r("gRPC specification", "https://grpc.io/docs/what-is-grpc/core-concepts/", "gRPC core concepts."),
				r("JSON-RPC specification", "https://www.jsonrpc.org/specification", "JSON-RPC 2.0 specification."),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "go-generics-zh",
			Query: "Go 泛型 性能",
			Expected: []core.SearchResult{
				r("Go 泛型性能实践", "https://go.dev/blog/generics-performance", "Go 泛型的性能基准与编译器开销。"),
				r("Go 泛型性能讨论", "https://github.com/golang/go/issues/12345", "Go 泛型性能的 issue 讨论与实测。"),
			},
			Distractors: []core.SearchResult{
				r("Java 泛型教程", "https://docs.oracle.com/javase/tutorial/java/generics/", "Java 泛型与类型擦除。"),
				r("C++ 模板参考", "https://en.cppreference.com/w/cpp/language/templates", "C++ 模板元编程。"),
				r("Python 类型注解", "https://docs.python.org/3/library/typing.html", "Python 类型提示与泛型。"),
				r("网页性能指标", "https://web.dev/vitals/", "Core Web Vitals 页面性能。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "react-vue-zh",
			Query: "React Vue 生态 对比",
			Expected: []core.SearchResult{
				r("React 生态概览", "https://react.dev/learn", "React 生态、工具链与对比。"),
				r("Vue 生态指南", "https://vuejs.org/guide/introduction.html", "Vue 生态与 React 的对比。"),
			},
			Distractors: []core.SearchResult{
				r("Angular 组件", "https://angular.io/guide/component-overview", "Angular 组件架构。"),
				r("Svelte 教程", "https://svelte.dev/tutorial", "Svelte 响应式教程。"),
				r("Webpack 配置", "https://webpack.js.org/configuration/", "Webpack 打包配置。"),
				r("npm 包仓库", "https://www.npmjs.com/", "JavaScript 包仓库。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "postgres-index-zh",
			Query: "PostgreSQL 索引 优化",
			Expected: []core.SearchResult{
				r("PostgreSQL 索引优化指南", "https://www.postgresql.org/docs/current/indexes.html", "PostgreSQL 索引类型与优化。"),
				r("PostgreSQL 生产索引优化", "https://pganalyze.com/blog/postgres-index-tuning", "生产环境的 PostgreSQL 索引优化。"),
			},
			Distractors: []core.SearchResult{
				r("MySQL 索引", "https://dev.mysql.com/doc/refman/8.0/en/mysql-indexes.html", "MySQL 索引文档。"),
				r("SQLite 查询优化", "https://www.sqlite.org/optoverview.html", "SQLite 查询规划器。"),
				r("MongoDB 索引", "https://www.mongodb.com/docs/manual/indexes/", "MongoDB 索引类型。"),
				r("Redis 持久化", "https://redis.io/docs/management/persistence/", "Redis 持久化模式。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "rust-async-zh",
			Query: "Rust 异步 运行时",
			Expected: []core.SearchResult{
				r("Rust 异步编程", "https://doc.rust-lang.org/book/ch17-00-async-await.html", "Rust 异步运行时与 Future。"),
				r("Tokio 异步运行时教程", "https://tokio.rs/tokio/tutorial", "Rust 的 Tokio 异步运行时。"),
			},
			Distractors: []core.SearchResult{
				r("Go goroutine", "https://go.dev/tour/concurrency/1", "Go goroutine 与 channel。"),
				r("Python asyncio", "https://docs.python.org/3/library/asyncio.html", "Python asyncio 事件循环。"),
				r("Node.js 事件循环", "https://nodejs.org/en/learn/asynchronous-work/event-loop-timers-and-nexttick", "Node.js 事件循环。"),
				r("Java 线程", "https://docs.oracle.com/javase/tutorial/essential/concurrency/", "Java 并发教程。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "k8s-eviction-zh",
			Query: "Kubernetes Pod 驱逐",
			Expected: []core.SearchResult{
				r("Kubernetes 节点压力驱逐", "https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/", "节点压力下的 Kubernetes Pod 驱逐。"),
				r("Kubernetes 驱逐 API issue", "https://github.com/kubernetes/kubernetes/issues/45678", "Kubernetes Pod 驱逐 API 行为。"),
			},
			Distractors: []core.SearchResult{
				r("Docker 容器生命周期", "https://docs.docker.com/engine/reference/commandline/container/", "Docker 容器生命周期命令。"),
				r("AWS ECS 任务放置", "https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-placement.html", "ECS 任务放置。"),
				r("Nomad 调度", "https://developer.hashicorp.com/nomad/docs/concepts/scheduling", "Nomad 调度概念。"),
				r("OpenShift 项目", "https://docs.openshift.com/container-platform/latest/applications/projects/working-with-projects.html", "OpenShift 项目。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "python-asyncio-timeout-zh",
			Query: "Python asyncio 超时",
			Expected: []core.SearchResult{
				r("Python asyncio 超时处理", "https://docs.python.org/3/library/asyncio-task.html#timeouts", "Python asyncio 的 timeout 与 wait_for。"),
				r("Python asyncio 超时模式", "https://realpython.com/async-io-python/", "Python 异步 IO 的超时模式。"),
			},
			Distractors: []core.SearchResult{
				r("Node.js 定时器", "https://nodejs.org/api/timers.html", "Node.js 定时器函数。"),
				r("Go context 取消", "https://pkg.go.dev/context", "Go context 的 deadline 与取消。"),
				r("Rust Tokio time", "https://docs.rs/tokio/latest/tokio/time/", "Tokio 时间工具。"),
				r("Java Future", "https://docs.oracle.com/javase/8/docs/api/java/util/concurrent/Future.html", "Java Future API。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "docker-multistage-zh",
			Query: "Docker 多阶段构建",
			Expected: []core.SearchResult{
				r("Docker 多阶段构建", "https://docs.docker.com/build/building/multi-stage/", "Docker 多阶段构建文档。"),
				r("Docker 多阶段构建示例", "https://github.com/docker/docs", "Docker 多阶段构建示例。"),
			},
			Distractors: []core.SearchResult{
				r("Podman 构建", "https://podman.io/docs", "Podman 容器构建文档。"),
				r("Buildah 教程", "https://buildah.io/", "Buildah 镜像构建教程。"),
				r("Kaniko 镜像构建", "https://github.com/GoogleContainerTools/kaniko", "Kaniko 集群内镜像构建。"),
				r("containerd 运行时", "https://containerd.io/docs/", "containerd 运行时文档。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "ts-satisfies-zh",
			Query: "TypeScript satisfies 运算符",
			Expected: []core.SearchResult{
				r("TypeScript satisfies 运算符", "https://www.typescriptlang.org/docs/handbook/2/satisfies-operator.html", "TypeScript satisfies 运算符参考。"),
				r("TypeScript 4.9 发布 satisfies", "https://devblogs.microsoft.com/typescript/announcing-typescript-4-9/", "TypeScript 4.9 引入 satisfies 运算符。"),
			},
			Distractors: []core.SearchResult{
				r("Babel 配置", "https://babeljs.io/docs/configuration", "Babel 配置参考。"),
				r("Flow 类型注解", "https://flow.org/en/docs/types/", "Flow 类型注解。"),
				r("JSDoc 参考", "https://jsdoc.app/", "JSDoc 标签与注解。"),
				r("ESLint 规则", "https://eslint.org/docs/latest/rules/", "ESLint 规则参考。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "sqlite-wal-zh",
			Query: "SQLite WAL 模式",
			Expected: []core.SearchResult{
				r("SQLite 预写日志", "https://www.sqlite.org/wal.html", "SQLite WAL 模式文档。"),
				r("SQLite WAL 实现", "https://github.com/sqlite/sqlite", "SQLite WAL 模式实现。"),
			},
			Distractors: []core.SearchResult{
				r("PostgreSQL 预写日志", "https://www.postgresql.org/docs/current/wal-intro.html", "PostgreSQL WAL 内部机制。"),
				r("MySQL InnoDB redo log", "https://dev.mysql.com/doc/refman/8.0/en/innodb-redo-log.html", "InnoDB redo log。"),
				r("RocksDB WAL", "https://github.com/facebook/rocksdb/wiki/Write-Ahead-Log", "RocksDB 预写日志。"),
				r("LevelDB 实现", "https://github.com/google/leveldb", "LevelDB 存储引擎。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
		{
			Name:  "mcp-spec-zh",
			Query: "MCP 协议 规范",
			Expected: []core.SearchResult{
				r("Model Context Protocol 规范", "https://modelcontextprotocol.io/specification", "MCP 协议规范与生命周期。"),
				r("MCP 规范仓库", "https://github.com/modelcontextprotocol/specification", "Model Context Protocol 规范源码。"),
			},
			Distractors: []core.SearchResult{
				r("OpenAI API 参考", "https://platform.openai.com/docs/api-reference", "OpenAI API 参考。"),
				r("Anthropic API 文档", "https://docs.anthropic.com/", "Anthropic API 文档。"),
				r("gRPC 规范", "https://grpc.io/docs/what-is-grpc/core-concepts/", "gRPC 核心概念。"),
				r("JSON-RPC 规范", "https://www.jsonrpc.org/specification", "JSON-RPC 2.0 规范。"),
			},
			MinHits: 2, MaxFirstRank: 3,
		},
	}
}
