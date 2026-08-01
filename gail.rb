# frozen_string_literal: true

class Gail < Formula
  desc "Tail -f viewer for the terminal"
  homepage "https://github.com/retroaalto/gail"
  url "https://github.com/retroaalto/gail/archive/refs/tags/v0.3.tar.gz"
  sha256 "6f66621dc4cee0b72fb2e6e2425dce63f71b548b276d481672a2bf191cf51787"
  license "MIT"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.Version=#{version}"
    system "go", "build", *std_go_args(ldflags:)
  end

  test do
    system "#{bin}/gail", "--version"
  end
end
