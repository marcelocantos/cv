#compdef cv

_cv() {
    local -a targets flags

    flags=(
        '-C[change to directory before doing anything]:dir:_directories'
        '-f[cvfile to read]:file:_files'
        '-v[verbose output]'
        '-B[unconditional rebuild]'
        '-n[dry run]'
        '-j[parallel jobs]:jobs:'
        '--why[explain why targets are stale]'
        '--graph[print dependency subgraph]'
        '--state[show build database entries]'
        '--help-agent[print the cv agents guide]'
        '--version[print version and exit]'
    )

    # Get targets and configs from cvfile
    targets=(${(f)"$(cv --complete 2>/dev/null)"})

    _arguments -s $flags '*:target:compadd -a targets'
}

_cv "$@"
