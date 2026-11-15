# Auto-updated by release.yml on tag v0.4.0-1. Do not edit manually.
class Keirouter < Formula
  desc "Self-hosted AI gateway — one endpoint for 130+ LLM providers with fallback, caching, and a dashboard"
  homepage "https://github.com/mydisha/keirouter"
  version "0.4.0-1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-1/keirouter_v0.4.0-1_darwin_arm64.tar.gz"
      sha256 "3a5f9f38207d110a4b1adce563e815379e105a6e34c7811c52f80ed916013e07"
    else
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-1/keirouter_v0.4.0-1_darwin_amd64.tar.gz"
      sha256 "7bf03e808284ca7da9307d2df2b1635f5abbd017b8c59924ef706baf9af1a6a5"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-1/keirouter_v0.4.0-1_linux_arm64.tar.gz"
      sha256 "5c5d850c2ed8ff99cfafef5b812f6286d2af0326562877254ac1e597cbc174a7"
    else
      url "https://github.com/mydisha/keirouter/releases/download/v0.4.0-1/keirouter_v0.4.0-1_linux_amd64.tar.gz"
      sha256 "2635be20b08c52e75a7c17193be66a7296c10344e9c57939cb65e57ecd505efa"
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
