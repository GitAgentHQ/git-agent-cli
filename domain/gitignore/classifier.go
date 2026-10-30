package gitignore

import "context"

// TechnologyClassifier decides which technologies a project uses.
//
// The candidate set is a closed list of Toptal gitignore identifiers, so this is
// a presence question per identifier and nothing else: no identifier is
// invented, and a classifier can be checked against a known project layout.
type TechnologyClassifier interface {
	ClassifyTechnologies(ctx context.Context, req ClassifyRequest) (*TechnologyVerdict, error)
}

// TechnologyVerdict is the classifier's answer and how sure it is. Confidence
// is the weakest presence probability the classifier reported, because an
// identifier that is wrongly included adds ignore rules for files the project
// does not have.
type TechnologyVerdict struct {
	Technologies []string `json:"technologies"`
	Confidence   float64  `json:"confidence"`
}

// ClassifyRequest carries the project evidence a classifier judges.
type ClassifyRequest struct {
	OS    string   `json:"os"`
	Dirs  []string `json:"dirs"`
	Files []string `json:"files"`
}

// CandidateTechnologies are the identifiers a classifier may return, grouped by
// the evidence that supports them. The classifier picks a subset; code owns
// this list so an answer cannot introduce an identifier Toptal does not serve.
var CandidateTechnologies = []TechnologyCandidate{
	// Operating systems.
	{ID: "macos", Group: "operating system", Evidence: "The project targets macOS."},
	{ID: "linux", Group: "operating system", Evidence: "The project targets Linux."},
	{ID: "windows", Group: "operating system", Evidence: "The project targets Windows."},
	{ID: "android", Group: "operating system", Evidence: "The project targets Android."},
	{ID: "ios", Group: "operating system", Evidence: "The project targets iOS."},
	// Languages.
	{ID: "go", Group: "language", Evidence: "The project is written in Go: a go.mod file or .go sources."},
	{ID: "node", Group: "language", Evidence: "The project uses Node.js: a package.json file."},
	{ID: "typescript", Group: "language", Evidence: "The project uses TypeScript: .ts or .tsx sources."},
	{ID: "python", Group: "language", Evidence: "The project is written in Python: .py sources."},
	{ID: "rust", Group: "language", Evidence: "The project is written in Rust: a Cargo.toml file."},
	{ID: "java", Group: "language", Evidence: "The project is written in Java: .java sources or Gradle or Maven files."},
	{ID: "kotlin", Group: "language", Evidence: "The project is written in Kotlin: .kt or .kts sources."},
	{ID: "ruby", Group: "language", Evidence: "The project is written in Ruby: .rb sources or a Gemfile."},
	{ID: "php", Group: "language", Evidence: "The project is written in PHP: .php sources."},
	{ID: "swift", Group: "language", Evidence: "The project is written in Swift: .swift sources."},
	{ID: "dart", Group: "language", Evidence: "The project is written in Dart: .dart sources or a pubspec.yaml."},
	{ID: "csharp", Group: "language", Evidence: "The project is written in C#: .cs sources or a .csproj file."},
	{ID: "objectivec", Group: "language", Evidence: "The project contains Objective-C: .m sources."},
	// Frameworks.
	{ID: "nextjs", Group: "framework", Evidence: "The project is a Next.js app: a next.config file or a next dependency."},
	{ID: "vue", Group: "framework", Evidence: "The project uses Vue: .vue sources or a vue dependency."},
	{ID: "svelte", Group: "framework", Evidence: "The project uses Svelte: .svelte sources."},
	{ID: "electron", Group: "framework", Evidence: "The project builds an Electron app: an electron dependency."},
	{ID: "django", Group: "framework", Evidence: "The project is a Django app: a manage.py file or a django dependency."},
	{ID: "flask", Group: "framework", Evidence: "The project is a Flask app: a flask dependency."},
	{ID: "rails", Group: "framework", Evidence: "The project is a Rails app: config/application.rb."},
	{ID: "flutter", Group: "framework", Evidence: "The project is a Flutter app: a flutter dependency in pubspec.yaml."},
	{ID: "xamarin", Group: "framework", Evidence: "The project builds with Xamarin: .xamarin or .sln files."},
	{ID: "qt", Group: "framework", Evidence: "The project uses Qt: .pro or .ui files."},
	// Package managers.
	{ID: "npm", Group: "package manager", Evidence: "The project installs packages with npm: a package-lock.json file."},
	{ID: "pnpm", Group: "package manager", Evidence: "The project installs packages with pnpm: a pnpm-lock.yaml file."},
	{ID: "yarn", Group: "package manager", Evidence: "The project installs packages with yarn: a yarn.lock file."},
	{ID: "bun", Group: "package manager", Evidence: "The project installs packages with bun: a bun.lockb file."},
	{ID: "composer", Group: "package manager", Evidence: "The project installs PHP packages with Composer: a composer.json file."},
	{ID: "bundler", Group: "package manager", Evidence: "The project installs Ruby gems with Bundler: a Gemfile.lock file."},
	{ID: "cargo", Group: "package manager", Evidence: "The project installs Rust crates with Cargo: a Cargo.lock file."},
	// Build tools.
	{ID: "makefile", Group: "build tool", Evidence: "The project builds with GNU Make: a Makefile."},
	{ID: "cmake", Group: "build tool", Evidence: "The project builds with CMake: a CMakeLists.txt file."},
	{ID: "bazel", Group: "build tool", Evidence: "The project builds with Bazel: BUILD or WORKSPACE files."},
	{ID: "gradle", Group: "build tool", Evidence: "The project builds with Gradle: build.gradle files."},
	{ID: "maven", Group: "build tool", Evidence: "The project builds with Maven: a pom.xml file."},
	{ID: "webpack", Group: "build tool", Evidence: "The project bundles with webpack: a webpack.config file."},
	{ID: "vite", Group: "build tool", Evidence: "The project builds with Vite: a vite.config file."},
	{ID: "esbuild", Group: "build tool", Evidence: "The project bundles with esbuild: an esbuild config or dependency."},
	// Infrastructure.
	{ID: "terraform", Group: "infrastructure", Evidence: "The project provisions infrastructure with Terraform: .tf files."},
	{ID: "ansible", Group: "infrastructure", Evidence: "The project configures hosts with Ansible: playbooks or role .yml files."},
	{ID: "docker", Group: "infrastructure", Evidence: "The project builds containers: a Dockerfile."},
	{ID: "kubernetes", Group: "infrastructure", Evidence: "The project deploys to Kubernetes: .yaml manifest files."},
	{ID: "helm", Group: "infrastructure", Evidence: "The project deploys with Helm: a Chart.yaml file."},
	{ID: "cloudfoundry", Group: "infrastructure", Evidence: "The project deploys to Cloud Foundry: a manifest.yml or Procfile."},
	{ID: "heroku", Group: "infrastructure", Evidence: "The project deploys to Heroku: a Procfile."},
	{ID: "firebase", Group: "infrastructure", Evidence: "The project uses Firebase: a firebase.json file."},
	{ID: "aws", Group: "infrastructure", Evidence: "The project uses AWS: an aws or serverless configuration file."},
	{ID: "gcp", Group: "infrastructure", Evidence: "The project uses Google Cloud: an app.yaml or .gcloudignore file."},
	{ID: "azure", Group: "infrastructure", Evidence: "The project uses Azure: Azure Pipelines files."},
	// Editors and IDEs.
	{ID: "jetbrains", Group: "editor", Evidence: "The project stores JetBrains IDE settings: a .idea directory."},
	{ID: "pycharm", Group: "editor", Evidence: "The project stores PyCharm settings: a .idea directory."},
	{ID: "visualstudiocode", Group: "editor", Evidence: "The project stores VS Code settings: a .vscode directory."},
	{ID: "xcode", Group: "editor", Evidence: "The project is an Xcode project: an .xcodeproj directory."},
	{ID: "eclipse", Group: "editor", Evidence: "The project stores Eclipse settings: .project or .classpath files."},
	{ID: "netcore", Group: "build tool", Evidence: "The project targets .NET: a .csproj or .fsproj file."},
}

// TechnologyCandidate is one closable option a classifier may select.
type TechnologyCandidate struct {
	ID       string
	Group    string
	Evidence string
}
