# Publish LinkedIn

## Execution Workflow

### Optimal Path
1. `agent-browser --cdp {{TWITTER_CDP_URL}} open https://www.linkedin.com/feed/`
   - prerequisites: authenticated LinkedIn session already available in the Chrome profile; use an isolated browser session (`share_browser=false`)
   - outputs: LinkedIn feed loaded in the active tab
   - on_error: if the page redirects to login, the CDP session is not authenticated; stop and reattach to the authenticated browser

2. `document.cookie.match(/JSESSIONID="([^"]+)"/)?.[1]`
   - prerequisites: the feed page is loaded
   - outputs: CSRF token string from the LinkedIn session cookie
   - data flow: browser session auth -> CSRF token for any in-page LinkedIn actions

3. Click the `Start a post` button via JS eval
   - prerequisites: feed page is open and authenticated
   - outputs: LinkedIn composer opens
   - on_error: if the button is not visible, refresh the feed page and retry

4. Insert the full approved article text into `.ql-editor` with:
   - `document.execCommand('insertText', false, text)`
   - prerequisites: composer is open; `text` must be the exact article body from `knowledgebase/content_engine/drafts/current_draft.md`
   - outputs: composer body populated with the article text
   - data flow: approved draft text -> `text` variable -> `.ql-editor`

5. Click the `Post` button via JS eval
   - prerequisites: composer body contains the complete article text
   - outputs: LinkedIn creates the post/article
   - on_error: if publish does not complete, do not retry blindly until the composer state is checked

6. Wait 4 seconds, then capture the live URL from the success toast `View post` link
   - prerequisites: publish succeeded
   - outputs: live LinkedIn article URL, typically a `/pulse/...` URL for long-form content
   - data flow: toast link -> `linkedin_published_url.json`

7. Write the live URL to `runs/iteration-30/manish/execution/step-14/linkedin_published_url.json`
   - exact JSON structure: `{ "url": "string" }`
   - example value: `{"url":"https://www.linkedin.com/pulse/nobody-raised-330m-lose-free-google-product-manish-prakash-mfeoc/?published=t"}`

## Data Flow

Approved article draft -> browser composer text -> published LinkedIn post -> toast `View post` URL -> `linkedin_published_url.json`

## Output File Formats

- File: `linkedin_published_url.json`
- Structure: `{ "url": "string" }`

## Failure To Avoid

- `Add media` image upload via CDP: not supported in this workflow. Do not attempt to attach images through the LinkedIn media picker. The human will attach the image manually after publishing.

## Notes

- This workflow publishes the long-form article as a fresh LinkedIn publish, not a draft.
- The successful result is a live LinkedIn article URL, not a feed draft link.
