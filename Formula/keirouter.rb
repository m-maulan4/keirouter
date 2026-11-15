# Auto-updated by release.yml on tag v0.4.0.1. Do not edit manually.
class Keirouter < Formula
  desc "Self-hosted AI gateway — one endpoint for 130+ LLM providers with fallback, caching, and a dashboard"
  homepage "https://github.com/mydisha/keirouter"
  version "0.4.0.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0.1/keirouter_v0.4.0.1_darwin_arm64.tar.gz"
      sha256 "c56e2a295e535e20a75a0d633854ba4cf645c74edce6beafccfb1fd10cc5aad7"
    else
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0.1/keirouter_v0.4.0.1_darwin_amd64.tar.gz"
      sha256 "3021e755d96d87dbeb70f0b12849db01b32a7a6024758f6d44aff6143463ac29"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0.1/keirouter_v0.4.0.1_linux_arm64.tar.gz"
      sha256 "86a6b4168c476db479b89dcdbc311335bdf62a5ecfc5b808b4684f406a532185"
    else
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0.1/keirouter_v0.4.0.1_linux_amd64.tar.gz"
      sha256 "c7a0685017b788a422a2cce03bd43f13f70631194046ef709186d8e3cdf58be2"
    end
  end

  def install
    bin.install "keirouter"
    (share/"keirouter").install "frontend"
  end

  service do
    run [opt_bin/"keirouter", "start", "--no-browser"]
    keep_alive true
    log_path var/"log/keirouter.log"
    error_log_path var/"log/keirouter.log"
  end

  def caveats
    <<~EOS
      Quick start:
        keirouter start                 # start server on :20180
        brew services start keirouter   # or run it in the background, on login

      Dashboard: http://localhost:20180  (default password: keirouter)
    EOS
  end

  test do
    assert_match "KeiRouter", shell_output("\#{bin}/keirouter --help 2>&1")
  end
end
