import asyncio
from unittest.mock import patch
import pytest
from agentworks_playwright.live import attach_live_browser, attach_live_browser_async

@pytest.mark.parametrize('origin', ['schedule', 'webhook', 'bot', 'pulse', 'notification'])
def test_unattended_explicit_helpers_skip_registration(monkeypatch, origin):
    monkeypatch.setenv('AGENTWORKS_EXECUTION_CONTEXT', origin)
    with patch('agentworks_playwright.live.websocket.create_connection', side_effect=AssertionError('network touched')):
        with attach_live_browser(object(), api_url='invalid') as live:
            assert live.session_id == ''
            live.stop()
        async def check():
            async with await attach_live_browser_async(object(), api_url='invalid') as live:
                assert live.session_id == ''
                await live.stop()
        asyncio.run(check())

def test_builder_still_requires_registration(monkeypatch):
    monkeypatch.setenv('AGENTWORKS_EXECUTION_CONTEXT', 'builder')
    monkeypatch.delenv('AGENTWORKS_LIVE_VIEW', raising=False)
    with patch('agentworks_playwright.live._Publisher', side_effect=RuntimeError('registration attempted')):
        class Context:
            browser = None
        with pytest.raises(RuntimeError, match='registration attempted'):
            attach_live_browser(Context())
