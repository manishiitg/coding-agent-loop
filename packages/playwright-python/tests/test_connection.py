import pytest
from agentworks_playwright.live import _endpoint


def test_credential_url_rejected():
    with pytest.raises(ValueError, match='without embedded credentials'):
        _endpoint('https://user:secret@example.test/s/run', 'test-token')


def test_session_required(monkeypatch):
    monkeypatch.delenv('MCP_SESSION_ID', raising=False)
    with pytest.raises(ValueError):
        _endpoint('https://example.test', 'test-token')


def test_endpoint_preserves_session_prefix():
    url, token = _endpoint('https://example.test/s/run', 'test-token', label='A test')
    assert url == 'wss://example.test/s/run/tools/browser/live?label=A+test'
    assert token == 'test-token'
