# frozen_string_literal: true

class Gail < Formula
  desc "Tail -f viewer for the terminal"
  homepage "https://github.com/retroaalto/gail"
  url "https://github.com/retroaalto/gail/archive/refs/tags/v0.5.tar.gz"
  sha256 "2483c0ab0459d1f21010ea701ea8add7f7a667d54c1ae6ca811f79910c0630df"
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
