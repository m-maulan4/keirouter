# Auto-updated by release.yml on tag v0.4.0-2. Do not edit manually.
class Keirouter < Formula
  desc "Self-hosted AI gateway — one endpoint for 130+ LLM providers with fallback, caching, and a dashboard"
  homepage "https://github.com/mydisha/keirouter"
  version "0.4.0-2"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-2/keirouter_v0.4.0-2_darwin_arm64.tar.gz"
      sha256 "0367f07fc3cb2fe2c089d09b9fdaa4c71babfba195d84e6bd8103c9605ae9020"
    else
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-2/keirouter_v0.4.0-2_darwin_amd64.tar.gz"
      sha256 "5c6788ee25580932b576dabbda5b4a35c7e2d75b361abf6250abecb7b000cbaa"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-2/keirouter_v0.4.0-2_linux_arm64.tar.gz"
      sha256 "e29375c66e804d8b38ee7812fc27b01ea130988190812a28b014d8dc29f4aca5"
    else
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-2/keirouter_v0.4.0-2_linux_amd64.tar.gz"
      sha256 "4288842193a22f3974843c16d3cf27828d01eac03c63ed4104b0d1facc663cf6"
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
