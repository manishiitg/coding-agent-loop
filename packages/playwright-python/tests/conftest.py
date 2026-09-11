import pytest

pytest_plugins = ['agentworks_playwright.pytest_plugin']

@pytest.fixture(scope='session')
def browser_context_args(browser_context_args):
    return {**browser_context_args, 'viewport': {'width': 640, 'height': 480}}
