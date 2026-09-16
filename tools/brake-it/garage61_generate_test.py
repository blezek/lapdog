import os
import unittest
from unittest import mock

import garage61_generate as generator


class Garage61RequestThrottleTest(unittest.TestCase):
  def setUp(self):
    self.previous_request_time = generator._last_request_started_at
    self.previous_rate_limit_time = generator._rate_limit_not_before
    generator._last_request_started_at = None
    generator._rate_limit_not_before = None

  def tearDown(self):
    generator._last_request_started_at = self.previous_request_time
    generator._rate_limit_not_before = self.previous_rate_limit_time

  def test_spaces_request_starts_by_configured_interval(self):
    with mock.patch.dict(os.environ, {generator.REQUEST_INTERVAL_ENV: "1.0"}):
      with mock.patch.object(generator.time, "monotonic", side_effect=(10.0, 10.25)):
        with mock.patch.object(generator.time, "sleep") as sleep:
          generator.throttle_request()
          generator.throttle_request()

    sleep.assert_called_once_with(0.75)
    self.assertEqual(11.0, generator._last_request_started_at)

  def test_rejects_invalid_request_interval(self):
    for value in ("not-a-number", "-1", "inf"):
      with self.subTest(value=value):
        with mock.patch.dict(os.environ, {generator.REQUEST_INTERVAL_ENV: value}):
          with self.assertRaisesRegex(RuntimeError, generator.REQUEST_INTERVAL_ENV):
            generator.request_interval_seconds()

  def test_request_interval_defaults_to_one_second(self):
    with mock.patch.dict(os.environ):
      os.environ.pop(generator.REQUEST_INTERVAL_ENV, None)
      self.assertEqual(1.0, generator.request_interval_seconds())

  def test_rejects_invalid_retry_jitter(self):
    with mock.patch.dict(os.environ, {generator.RETRY_JITTER_ENV: "-0.1"}):
      with self.assertRaisesRegex(RuntimeError, generator.RETRY_JITTER_ENV):
        generator.retry_jitter_seconds()

  def test_request_throttles_initial_attempt_and_rate_limit_retry(self):
    responses = (
      generator.FetchResponse(
        429,
        '{"details":{"retryAfterSeconds":2}}',
        {"retry-after": "3"},
      ),
      generator.FetchResponse(200, "{}", {}),
    )
    with mock.patch.dict(os.environ, {generator.REQUEST_INTERVAL_ENV: "0"}):
      with mock.patch.object(generator, "urllib_fetch", side_effect=responses):
        with mock.patch.object(generator.random, "uniform", return_value=0.25):
          with mock.patch.object(
            generator.time,
            "monotonic",
            side_effect=(10.0, 10.0, 10.0, 13.25),
          ):
            with mock.patch.object(generator.time, "sleep") as sleep:
              self.assertEqual({}, generator.request("token", "/tracks"))

    sleep.assert_called_once_with(3.25)

  def test_successful_retry_after_delays_the_following_request(self):
    responses = (
      generator.FetchResponse(200, "{}", {"retry-after": "4"}),
      generator.FetchResponse(200, "{}", {}),
    )
    with mock.patch.dict(os.environ, {generator.REQUEST_INTERVAL_ENV: "0"}):
      with mock.patch.object(generator, "urllib_fetch", side_effect=responses):
        with mock.patch.object(generator.random, "uniform", return_value=0.5):
          with mock.patch.object(
            generator.time,
            "monotonic",
            side_effect=(20.0, 20.0, 20.0, 24.5),
          ):
            with mock.patch.object(generator.time, "sleep") as sleep:
              self.assertEqual({}, generator.request("token", "/tracks"))
              self.assertEqual({}, generator.request("token", "/cars"))

    sleep.assert_called_once_with(4.5)

  def test_request_logs_elapsed_time_to_one_decimal_place(self):
    response = generator.FetchResponse(200, "{}", {})
    with mock.patch.object(generator, "throttle_request"):
      with mock.patch.object(generator, "urllib_fetch", return_value=response):
        with mock.patch.object(generator.time, "perf_counter", side_effect=(10.04, 11.30)):
          with mock.patch.object(generator.LOGGER, "info") as info:
            self.assertEqual({}, generator.request("token", "/tracks"))

    info.assert_called_once_with("Garage61 GET /tracks -> HTTP 200 | 1.3s")

  def test_urllib_fetch_returns_normalized_response_headers(self):
    response = mock.MagicMock()
    response.status = 200
    response.read.return_value = b"{}"
    response.headers = {"Retry-After": "5", "X-RateLimit-Hit": "bucket"}
    context = mock.MagicMock()
    context.__enter__.return_value = response

    with mock.patch.object(generator.urllib.request, "urlopen", return_value=context):
      got = generator.urllib_fetch("https://example.test", "token")

    self.assertEqual(200, got.status)
    self.assertEqual("{}", got.body)
    self.assertEqual("5", got.headers["retry-after"])
    self.assertEqual("bucket", got.headers["x-ratelimit-hit"])

  def test_parses_only_final_curl_response_headers(self):
    headers = generator.parse_curl_headers(
      "HTTP/1.1 302 Found\r\nRetry-After: 99\r\nLocation: /next\r\n\r\n"
      "HTTP/2 200\r\nX-Ratelimit-Global-Remaining: 0\r\nRetry-After: 7\r\n\r\n",
    )

    self.assertEqual("7", headers["retry-after"])
    self.assertEqual("0", headers["x-ratelimit-global-remaining"])
    self.assertNotIn("location", headers)

  def test_429_without_a_valid_server_delay_uses_ten_second_fallback(self):
    response = generator.FetchResponse(429, "not-json", {"retry-after": "invalid"})

    self.assertEqual(10.0, generator.response_retry_seconds(response))


if __name__ == "__main__":
  unittest.main()
