# npm license overrides

The frontend license generator fails the image build when an included frontend component
does not contain a distributable license or notice file. A directory named for
the exact normalized `package@version` may supply a tracked exception.

`react-remove-scroll-bar@2.3.8` declares MIT in the locked npm metadata but its
published tarball omits the license file. The tracked text is sourced from the
official upstream repository's `LICENSE` at commit
`7301c160fda44cb8cf2b9fdfde61efad35736196`.
