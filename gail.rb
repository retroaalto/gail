# frozen_string_literal: true

class Gail < Formula
  desc "Tail -f viewer for the terminal"
  homepage "https://github.com/retroaalto/gail"
  url "https://github.com/retroaalto/gail/archive/refs/tags/v0.3.tar.gz"
  sha256 "9e1d6f193bbbd0d3273bfac1372ca04e66997c0bb8410c0df21bf5059f9a09c4"
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
