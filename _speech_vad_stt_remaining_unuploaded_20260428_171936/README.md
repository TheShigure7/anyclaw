# Speech VAD/STT Remaining Unuploaded Export

This folder collects the speech-related changes exported from branch:

- `feature/speech-vad-stt-phase1`

It follows the same idea as the existing `_gateway_remaining_unuploaded_*` folders:

- group changed files by topic
- preserve relative project paths under each group
- make it easy to review or move patches in batches

## Groups

### 01_vad_provider_and_voicewake

Contains the VAD-side refactor and default provider switch:

- provider abstraction for VAD
- fallback heuristic VAD retained
- WebRTC VAD implementation added
- `VoiceWake` switched to use the provider interface and default WebRTC path

### 02_stt_openai_client_refactor

Contains the OpenAI STT cleanup:

- multipart/request logic extracted into a thin client
- `WhisperProvider` simplified to reuse the shared client

### 03_stt_google_official_client

Contains the Google STT migration:

- provider moved from handwritten REST calls to the official Google Speech Go client
- thin Google client wrapper added
- provider config extended with `CredentialsJSON`
- tests updated to use fake client injection
- `go.mod` / `go.sum` included because the dependency graph changed

## Validation

The branch state corresponding to these files passed:

- `go test ./pkg/speech/...`
- `go test ./pkg/gateway/...`
