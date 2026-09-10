import os
from playwright.sync_api import expect


def test_live_page(page, agentworks_live_browser):
    assert agentworks_live_browser.session_id.startswith('pw-')
    page.set_content('<body style="background:#123456"><button onclick="document.body.style.background=\'#285430\'">Confirm</button></body>')
    page.wait_for_timeout(1500)
    page.get_by_role('button', name='Confirm').click()
    page.wait_for_timeout(1500)
    popup = page.context.new_page()
    popup.set_content('<body style="background:#cca020">Popup</body>')
    page.wait_for_timeout(1500)
    popup.close()
    page.wait_for_timeout(1500)
    expect(page.get_by_role('button', name='Confirm')).to_be_visible()
    if os.environ.get('AGENTWORKS_TEST_FAILURE') == '1':
        assert False, 'intentional assertion failure to verify fixture cleanup'
