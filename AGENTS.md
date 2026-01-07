# ad-editor Repository Guide

This is a Rust terminal text editor project with a focus on modularity and testing.

## Build, Lint, and Test Commands

### Building
- `cargo build` - Build the project
- `cargo build --release` - Build optimized release binary
- `just format` - Format all Rust code using rustfmt

### Linting
- `cargo clippy --workspace --all-targets --all-features --examples --tests -- -D warnings` - Run clippy
- `just check-clippy` - Alias for above
- `just check-fmt` - Check code formatting without modifying
- `just check-docs` - Generate docs and check for warnings
- `just check-spelling` - Run typos spell checker
- `just check-all` - Run all checks (clippy, fmt, docs, spelling)
- `just fix-spelling` - Auto-fix spelling errors

### Testing
- `cargo nextest run --workspace` - Run all tests using nextest
- `just test` - Alias for nextest
- `just test FILTER` - Run tests matching filter pattern
- `cargo nextest run --workspace <test_name>` - Run single test by name
- `cargo test --lib <test_name>` - Run specific library test
- `cargo test -- <test_name>` - Run specific test with cargo
- `just watch-tests` - Watch mode: re-run tests on file changes (uses entr)

### Documentation
- `cargo doc --all-features --open` - Build and open documentation
- `just open-docs` - Alias for above
- `export RUSTDOCFLAGS="-D warnings -D rustdoc::broken-intra-doc-links"` is set for strict docs

### Other
- `cargo bench` - Run Criterion benchmarks
- `just bench` - Run benchmarks and open HTML report
- `just fuzz` - Run fuzzing (requires fuzz feature)
- `just todo` - Search for TODO/FIXME comments

## Code Style Guidelines

### Imports
- Organize imports in alphabetical groups: std, external crates, internal modules
- Use `{ crate::module }` for internal crate imports
- Prefer qualified imports (e.g., `std::io::Write`) unless used frequently
- Keep imports sorted within groups

### Formatting
- Use `cargo fmt` (2 spaces indentation)
- Line length: No strict limit, but prefer readable lines (~100-120 chars)
- Use rustfmt default settings
- Never add trailing whitespace

### Types
- Use explicit types for public APIs, let inference handle locals
- Prefer `Result<T, String>` or custom error types over `anyhow::Error`
- Use `Arc<RwLock<T>>` for shared mutable state
- Leverage `ReadOnlyLock<T>` wrapper for read-only shared access
- Prefer concrete types in function signatures, generics when reusable

### Naming Conventions
- Types: `PascalCase` (structs, enums, traits)
- Functions/Methods: `snake_case`
- Constants: `SCREAMING_SNAKE_CASE`
- Private module-level constants: `snake_case`
- Variables: `snake_case`
- Acronyms: Treat as words (e.g., `TsState` not `TSState`, `lsp` not `LSP`)
- Module organization: Logical grouping, minimal dependencies

### Error Handling
- Use `Result<T, String>` for simple error cases
- Use `Option<T>` where absence is valid
- Prefer early returns with `?` operator
- Use `tracing::error!` for logging errors, not just returning
- Validate inputs at API boundaries
- Return user-friendly error messages from editor actions

### Documentation
- Use module-level `//!` comments for high-level docs
- Document public APIs with `///`
- Use doc examples for complex functionality
- Keep documentation concise but complete
- Reference related functions and modules

### Patterns and Best Practices
- Use builder pattern for complex initialization
- Prefer composition over inheritance
- Use traits for shared behavior, traits objects sparingly
- Implement `Default` for common initialization
- Use `Arc` for sharing, `RwLock` for interior mutability
- Minimize `unsafe` - document rationale when used
- Use iterator methods (map, filter, etc.) over loops
- Prefer `format!` over string concatenation
- Use `tracing` for logging (debug, info, error levels)

### Testing
- Use `simple_test_case::test_case` for parameterized tests
- Write data-driven scenario tests in `tests/data/editor-scenarios/`
- Use `assert_fs::TempDir` for temp file handling
- Keep tests focused and readable
- Test edge cases (empty input, boundaries, invalid data)
- Use `cargo nextest` for parallel test execution

### Code Organization
- Main binary: `src/main.rs`
- Library code: `src/lib.rs`
- Module structure mirrors functionality (buffer, editor, lsp, etc.)
- Workspace includes: `crates/*`, `fuzz`, `xtask`
- Tests co-located with code in modules
- Integration tests in `tests/`

### Macros
- Use `#[macro_export]` for reusable macros
- Macro names: `snake_case` like functions
- Document macro expansion behavior
- Prefer procedural macros over declarative when appropriate

### Concurrency
- Use channels (`std::sync::mpsc`) for message passing
- Prefer `Arc<RwLock<T>>` over `Arc<Mutex<T>>` for read-heavy workloads
- Document lock ordering to prevent deadlocks
- Use `OnceLock` for lazy static initialization

### Build Configuration
- Edition: 2024
- Features: `fuzz` (optional, for fuzzing)
- Profiles: `release`, `flamegraph` (for profiling), `release-test` (faster release builds)
- LTO enabled for release builds