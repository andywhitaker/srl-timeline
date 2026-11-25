#!/usr/bin/env python3
"""Nokia SR Linux CLI plugin registering `tools timeline` to launch the interactive TUI."""

import os
import subprocess
import sys

from srlinux.mgmt.cli.idle_timer import get_idle_task
from srlinux.mgmt.cli.required_plugin import RequiredPlugin
from srlinux.mgmt.cli.tools_plugin import ToolsPlugin
from srlinux.syntax import Syntax

TIMELINE_BIN = '/usr/local/bin/timeline'

BUILTIN_THEMES = (
    'dark',
    'cobalt2',
    'monokai',
    'solarized-dark',
    'dracula',
    'nord',
    'catppuccin-mocha',
    'tokyo-night',
    'gruvbox-dark',
    'light',
    'solarized-light',
)

COLOR_PROFILES = (
    'truecolor',
    '256',
    'ansi',
    'ascii',
)


class Plugin(ToolsPlugin):
    """Adds `tools timeline` command under SR Linux tools mode."""

    def get_required_plugins(self):
        return [RequiredPlugin('tools_mode')]

    def on_tools_load(self, state):
        tools_root = state.command_tree.tools_mode.root
        syntax = Syntax(
            'timeline',
            help='Launch interactive configuration timeline & rollback TUI',
        )
        syntax.add_named_argument(
            'filter',
            default=None,
            help='Initial path or keyword filter (e.g. bgp, interface, acl)',
        )
        syntax.add_named_argument(
            'theme',
            default=None,
            suggestions=self._suggest_themes,
            help='Color theme name (e.g. dark, light, nord, tokyo-night)',
        )
        syntax.add_named_argument(
            'color-profile',
            default=None,
            suggestions=self._suggest_color_profiles,
            help='Terminal color profile (truecolor, 256, ansi, ascii)',
        )
        tools_root.add_command(
            syntax,
            update_location=False,
            callback=self._launch_timeline,
        )

    @staticmethod
    def _suggest_themes(*_args, **_kwargs):
        return list(BUILTIN_THEMES)

    @staticmethod
    def _suggest_color_profiles(*_args, **_kwargs):
        return list(COLOR_PROFILES)

    @staticmethod
    def _launch_timeline(state, input, output, arguments, **_kwargs):
        if not os.access(TIMELINE_BIN, os.X_OK):
            output.print_error_line(
                f'srl-timeline binary not found or not executable at {TIMELINE_BIN}'
            )
            return 1

        if not state.is_interactive or not (sys.stdin.isatty() and sys.stdout.isatty()):
            output.print_error_line(
                'tools timeline requires an interactive terminal (TTY). '
                'Run it from an interactive sr_cli session, or use `bash timeline log` / '
                '`bash timeline diff` for non-interactive output.'
            )
            return 1

        cmd = [TIMELINE_BIN]

        filter_val = arguments.get('filter')
        if filter_val:
            cmd.extend(['--filter', str(filter_val)])

        theme_val = arguments.get('theme')
        if theme_val:
            cmd.extend(['--theme', str(theme_val)])

        profile_val = arguments.get('color-profile')
        if profile_val:
            cmd.extend(['--color-profile', str(profile_val)])

        child_env = os.environ.copy()
        if not child_env.get('TERM') or child_env.get('TERM') == 'dumb':
            child_env['TERM'] = 'xterm-256color'

        idle_task = None
        try:
            idle_task = get_idle_task()
            if idle_task is not None:
                idle_task.set_idle_timeout(0)
            rc = subprocess.call(cmd, env=child_env)
        except Exception as exc:
            output.print_error_line(f'Failed to launch timeline TUI: {exc}')
            return 1
        finally:
            if idle_task is not None:
                idle_task.set_idle_timeout()

        if rc != 0:
            output.print_error_line(f'timeline exited with status {rc}')
        return rc
