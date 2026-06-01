_cv() {
    local cur="${COMP_WORDS[COMP_CWORD]}"

    # Complete flags
    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "-C -f -v -B -n -j --why --graph --state --help-agent --version" -- "$cur"))
        return
    fi

    # Complete targets and configs from cvfile
    local targets
    targets=$(cv --complete 2>/dev/null)
    COMPREPLY=($(compgen -W "$targets" -- "$cur"))
}

complete -F _cv cv
