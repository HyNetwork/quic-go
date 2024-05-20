package wire

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	mrand "math/rand"
	"testing"
	"time"

	"github.com/apernet/quic-go/internal/protocol"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Header Parsing", func() {
	Context("Parsing the Connection ID", func() {
		It("parses the connection ID of a long header packet", func() {
			b, err := (&ExtendedHeader{
				Header: Header{
					Type:             protocol.PacketTypeHandshake,
					DestConnectionID: protocol.ParseConnectionID([]byte{0xde, 0xca, 0xfb, 0xad}),
					SrcConnectionID:  protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6}),
					Version:          protocol.Version1,
				},
				PacketNumberLen: 2,
			}).Append(nil, protocol.Version1)
			Expect(err).ToNot(HaveOccurred())
			connID, err := ParseConnectionID(b, 8)
			Expect(err).ToNot(HaveOccurred())
			Expect(connID).To(Equal(protocol.ParseConnectionID([]byte{0xde, 0xca, 0xfb, 0xad})))
		})

		It("errors on EOF, for long header packets", func() {
			b, err := (&ExtendedHeader{
				Header: Header{
					Type:             protocol.PacketTypeHandshake,
					DestConnectionID: protocol.ParseConnectionID([]byte{0xde, 0xca, 0xfb, 0xad, 0x13, 0x37}),
					SrcConnectionID:  protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 8, 9}),
					Version:          protocol.Version1,
				},
				PacketNumberLen: 2,
			}).Append(nil, protocol.Version1)
			Expect(err).ToNot(HaveOccurred())
			data := b[:len(b)-2] // cut the packet number
			_, err = ParseConnectionID(data, 8)
			Expect(err).ToNot(HaveOccurred())
			for i := 0; i < 1 /* first byte */ +4 /* version */ +1 /* conn ID lengths */ +6; /* dest conn ID */ i++ {
				b := make([]byte, i)
				copy(b, data[:i])
				_, err := ParseConnectionID(b, 8)
				Expect(err).To(MatchError(io.EOF))
			}
		})

		It("errors when encountering a too long connection ID", func() {
			b := []byte{0x80, 0, 0, 0, 0}
			binary.BigEndian.PutUint32(b[1:], uint32(protocol.Version1))
			b = append(b, 21) // dest conn id len
			b = append(b, make([]byte, 21)...)
			_, err := ParseConnectionID(b, 4)
			Expect(err).To(MatchError(protocol.ErrInvalidConnectionIDLen))
		})
	})

	Context("identifying 0-RTT packets", func() {
		It("recognizes 0-RTT packets, for QUIC v1", func() {
			zeroRTTHeader := make([]byte, 5)
			zeroRTTHeader[0] = 0x80 | 0b01<<4
			binary.BigEndian.PutUint32(zeroRTTHeader[1:], uint32(protocol.Version1))

			Expect(Is0RTTPacket(zeroRTTHeader)).To(BeTrue())
			Expect(Is0RTTPacket(zeroRTTHeader[:4])).To(BeFalse())                           // too short
			Expect(Is0RTTPacket([]byte{zeroRTTHeader[0], 1, 2, 3, 4})).To(BeFalse())        // unknown version
			Expect(Is0RTTPacket([]byte{zeroRTTHeader[0] | 0x80, 1, 2, 3, 4})).To(BeFalse()) // short header
			Expect(Is0RTTPacket(append(zeroRTTHeader, []byte("foobar")...))).To(BeTrue())
		})

		It("recognizes 0-RTT packets, for QUIC v2", func() {
			zeroRTTHeader := make([]byte, 5)
			zeroRTTHeader[0] = 0x80 | 0b10<<4
			binary.BigEndian.PutUint32(zeroRTTHeader[1:], uint32(protocol.Version2))

			Expect(Is0RTTPacket(zeroRTTHeader)).To(BeTrue())
			Expect(Is0RTTPacket(zeroRTTHeader[:4])).To(BeFalse())                           // too short
			Expect(Is0RTTPacket([]byte{zeroRTTHeader[0], 1, 2, 3, 4})).To(BeFalse())        // unknown version
			Expect(Is0RTTPacket([]byte{zeroRTTHeader[0] | 0x80, 1, 2, 3, 4})).To(BeFalse()) // short header
			Expect(Is0RTTPacket(append(zeroRTTHeader, []byte("foobar")...))).To(BeTrue())
		})
	})
	Context("parsing the version", func() {
		It("parses the version", func() {
			b := []byte{0x80, 0xde, 0xad, 0xbe, 0xef}
			v, err := ParseVersion(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(v).To(Equal(protocol.Version(0xdeadbeef)))
		})

		It("errors with EOF", func() {
			b := []byte{0x80, 0xde, 0xad, 0xbe, 0xef}
			_, err := ParseVersion(b)
			Expect(err).ToNot(HaveOccurred())
			for i := range b {
				_, err := ParseVersion(b[:i])
				Expect(err).To(MatchError(io.EOF))
			}
		})
	})

	Context("parsing arbitrary length connection IDs", func() {
		generateConnID := func(l int) protocol.ArbitraryLenConnectionID {
			c := make(protocol.ArbitraryLenConnectionID, l)
			rand.Read(c)
			return c
		}

		generatePacket := func(src, dest protocol.ArbitraryLenConnectionID) []byte {
			b := []byte{0x80, 1, 2, 3, 4}
			b = append(b, uint8(dest.Len()))
			b = append(b, dest.Bytes()...)
			b = append(b, uint8(src.Len()))
			b = append(b, src.Bytes()...)
			return b
		}

		It("parses arbitrary length connection IDs", func() {
			src := generateConnID(mrand.Intn(255) + 1)
			dest := generateConnID(mrand.Intn(255) + 1)
			b := generatePacket(src, dest)
			l := len(b)
			b = append(b, []byte("foobar")...) // add some payload

			parsed, d, s, err := ParseArbitraryLenConnectionIDs(b)
			Expect(parsed).To(Equal(l))
			Expect(err).ToNot(HaveOccurred())
			Expect(s).To(Equal(src))
			Expect(d).To(Equal(dest))
		})

		It("errors on EOF", func() {
			b := generatePacket(generateConnID(mrand.Intn(255)+1), generateConnID(mrand.Intn(255)+1))
			_, _, _, err := ParseArbitraryLenConnectionIDs(b)
			Expect(err).ToNot(HaveOccurred())

			for i := range b {
				_, _, _, err := ParseArbitraryLenConnectionIDs(b[:i])
				Expect(err).To(MatchError(io.EOF))
			}
		})
	})

	Context("Identifying Version Negotiation Packets", func() {
		It("identifies version negotiation packets", func() {
			Expect(IsVersionNegotiationPacket([]byte{0x80 | 0x56, 0, 0, 0, 0})).To(BeTrue())
			Expect(IsVersionNegotiationPacket([]byte{0x56, 0, 0, 0, 0})).To(BeFalse())
			Expect(IsVersionNegotiationPacket([]byte{0x80, 1, 0, 0, 0})).To(BeFalse())
			Expect(IsVersionNegotiationPacket([]byte{0x80, 0, 1, 0, 0})).To(BeFalse())
			Expect(IsVersionNegotiationPacket([]byte{0x80, 0, 0, 1, 0})).To(BeFalse())
			Expect(IsVersionNegotiationPacket([]byte{0x80, 0, 0, 0, 1})).To(BeFalse())
		})

		It("returns false on EOF", func() {
			vnp := []byte{0x80, 0, 0, 0, 0}
			for i := range vnp {
				Expect(IsVersionNegotiationPacket(vnp[:i])).To(BeFalse())
			}
		})
	})

	Context("Long Headers", func() {
		It("parses a Long Header", func() {
			destConnID := protocol.ParseConnectionID([]byte{9, 8, 7, 6, 5, 4, 3, 2, 1})
			srcConnID := protocol.ParseConnectionID([]byte{0xde, 0xad, 0xbe, 0xef})
			data := []byte{0xc0 ^ 0x3}
			data = appendVersion(data, protocol.Version1)
			data = append(data, 0x9) // dest conn id length
			data = append(data, destConnID.Bytes()...)
			data = append(data, 0x4) // src conn id length
			data = append(data, srcConnID.Bytes()...)
			data = append(data, encodeVarInt(6)...)  // token length
			data = append(data, []byte("foobar")...) // token
			data = append(data, encodeVarInt(10)...) // length
			hdrLen := len(data)
			data = append(data, []byte{0, 0, 0xbe, 0xef}...) // packet number
			data = append(data, []byte("foobar")...)
			Expect(IsVersionNegotiationPacket(data)).To(BeFalse())

			hdr, pdata, rest, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(pdata).To(Equal(data))
			Expect(hdr.DestConnectionID).To(Equal(destConnID))
			Expect(hdr.SrcConnectionID).To(Equal(srcConnID))
			Expect(hdr.Type).To(Equal(protocol.PacketTypeInitial))
			Expect(hdr.Token).To(Equal([]byte("foobar")))
			Expect(hdr.Length).To(Equal(protocol.ByteCount(10)))
			Expect(hdr.Version).To(Equal(protocol.Version1))
			Expect(rest).To(BeEmpty())
			b := bytes.NewReader(data)
			extHdr, err := hdr.ParseExtended(b, protocol.Version1)
			Expect(err).ToNot(HaveOccurred())
			Expect(extHdr.PacketNumber).To(Equal(protocol.PacketNumber(0xbeef)))
			Expect(extHdr.PacketNumberLen).To(Equal(protocol.PacketNumberLen4))
			Expect(b.Len()).To(Equal(6)) // foobar
			Expect(hdr.ParsedLen()).To(BeEquivalentTo(hdrLen))
			Expect(extHdr.ParsedLen()).To(Equal(hdr.ParsedLen() + 4))
		})

		It("errors if 0x40 is not set", func() {
			data := []byte{
				0x80 | 0x2<<4,
				0x11,                   // connection ID lengths
				0xde, 0xca, 0xfb, 0xad, // dest conn ID
				0xde, 0xad, 0xbe, 0xef, // src conn ID
			}
			_, _, _, err := ParsePacket(data)
			Expect(err).To(MatchError("not a QUIC packet"))
		})

		It("stops parsing when encountering an unsupported version", func() {
			data := []byte{
				0xc0,
				0xde, 0xad, 0xbe, 0xef,
				0x8,                                    // dest conn ID len
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, // dest conn ID
				0x8,                                    // src conn ID len
				0x8, 0x7, 0x6, 0x5, 0x4, 0x3, 0x2, 0x1, // src conn ID
				'f', 'o', 'o', 'b', 'a', 'r', // unspecified bytes
			}
			hdr, _, rest, err := ParsePacket(data)
			Expect(err).To(MatchError(ErrUnsupportedVersion))
			Expect(hdr.Version).To(Equal(protocol.Version(0xdeadbeef)))
			Expect(hdr.DestConnectionID).To(Equal(protocol.ParseConnectionID([]byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8})))
			Expect(hdr.SrcConnectionID).To(Equal(protocol.ParseConnectionID([]byte{0x8, 0x7, 0x6, 0x5, 0x4, 0x3, 0x2, 0x1})))
			Expect(rest).To(BeEmpty())
		})

		It("parses a Long Header without a destination connection ID", func() {
			data := []byte{0xc0 ^ 0x1<<4}
			data = appendVersion(data, protocol.Version1)
			data = append(data, 0)                                 // dest conn ID len
			data = append(data, 4)                                 // src conn ID len
			data = append(data, []byte{0xde, 0xad, 0xbe, 0xef}...) // source connection ID
			data = append(data, encodeVarInt(0)...)                // length
			data = append(data, []byte{0xde, 0xca, 0xfb, 0xad}...)
			hdr, _, _, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.Type).To(Equal(protocol.PacketType0RTT))
			Expect(hdr.SrcConnectionID).To(Equal(protocol.ParseConnectionID([]byte{0xde, 0xad, 0xbe, 0xef})))
			Expect(hdr.DestConnectionID).To(BeZero())
		})

		It("parses a Long Header without a source connection ID", func() {
			data := []byte{0xc0 ^ 0x2<<4}
			data = appendVersion(data, protocol.Version1)
			data = append(data, 10)                                       // dest conn ID len
			data = append(data, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}...) // dest connection ID
			data = append(data, 0)                                        // src conn ID len
			data = append(data, encodeVarInt(0)...)                       // length
			data = append(data, []byte{0xde, 0xca, 0xfb, 0xad}...)
			hdr, _, _, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.SrcConnectionID).To(BeZero())
			Expect(hdr.DestConnectionID).To(Equal(protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})))
		})

		It("parses a Long Header without a too long destination connection ID", func() {
			data := []byte{0xc0 ^ 0x2<<4}
			data = appendVersion(data, protocol.Version1)
			data = append(data, 21)                                                                                   // dest conn ID len
			data = append(data, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21}...) // dest connection ID
			data = append(data, 0x0)                                                                                  // src conn ID len
			data = append(data, encodeVarInt(0)...)                                                                   // length
			data = append(data, []byte{0xde, 0xca, 0xfb, 0xad}...)
			_, _, _, err := ParsePacket(data)
			Expect(err).To(MatchError(protocol.ErrInvalidConnectionIDLen))
		})

		It("parses a Long Header with a 2 byte packet number", func() {
			data := []byte{0xc0 ^ 0x1}
			data = appendVersion(data, protocol.Version1) // version number
			data = append(data, []byte{0x0, 0x0}...)      // connection ID lengths
			data = append(data, encodeVarInt(0)...)       // token length
			data = append(data, encodeVarInt(0)...)       // length
			data = append(data, []byte{0x1, 0x23}...)

			hdr, _, _, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			b := bytes.NewReader(data)
			extHdr, err := hdr.ParseExtended(b, protocol.Version1)
			Expect(err).ToNot(HaveOccurred())
			Expect(extHdr.PacketNumber).To(Equal(protocol.PacketNumber(0x123)))
			Expect(extHdr.PacketNumberLen).To(Equal(protocol.PacketNumberLen2))
			Expect(b.Len()).To(BeZero())
		})

		It("parses a Retry packet, for QUIC v1", func() {
			data := []byte{0xc0 | 0b11<<4 | (10 - 3) /* connection ID length */}
			data = appendVersion(data, protocol.Version1)
			data = append(data, []byte{6}...)                             // dest conn ID len
			data = append(data, []byte{6, 5, 4, 3, 2, 1}...)              // dest conn ID
			data = append(data, []byte{10}...)                            // src conn ID len
			data = append(data, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}...) // source connection ID
			data = append(data, []byte{'f', 'o', 'o', 'b', 'a', 'r'}...)  // token
			data = append(data, []byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}...)
			hdr, pdata, rest, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.Type).To(Equal(protocol.PacketTypeRetry))
			Expect(hdr.Version).To(Equal(protocol.Version1))
			Expect(hdr.DestConnectionID).To(Equal(protocol.ParseConnectionID([]byte{6, 5, 4, 3, 2, 1})))
			Expect(hdr.SrcConnectionID).To(Equal(protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})))
			Expect(hdr.Token).To(Equal([]byte("foobar")))
			Expect(pdata).To(Equal(data))
			Expect(rest).To(BeEmpty())
		})

		It("parses a Retry packet, for QUIC v2", func() {
			data := []byte{0xc0 | 0b00<<4 | (10 - 3) /* connection ID length */}
			data = appendVersion(data, protocol.Version2)
			data = append(data, []byte{6}...)                             // dest conn ID len
			data = append(data, []byte{6, 5, 4, 3, 2, 1}...)              // dest conn ID
			data = append(data, []byte{10}...)                            // src conn ID len
			data = append(data, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}...) // source connection ID
			data = append(data, []byte{'f', 'o', 'o', 'b', 'a', 'r'}...)  // token
			data = append(data, []byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}...)
			hdr, pdata, rest, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.Type).To(Equal(protocol.PacketTypeRetry))
			Expect(hdr.Version).To(Equal(protocol.Version2))
			Expect(hdr.DestConnectionID).To(Equal(protocol.ParseConnectionID([]byte{6, 5, 4, 3, 2, 1})))
			Expect(hdr.SrcConnectionID).To(Equal(protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})))
			Expect(hdr.Token).To(Equal([]byte("foobar")))
			Expect(pdata).To(Equal(data))
			Expect(rest).To(BeEmpty())
		})

		It("errors if the Retry packet is too short for the integrity tag", func() {
			data := []byte{0xc0 | 0x3<<4 | (10 - 3) /* connection ID length */}
			data = appendVersion(data, protocol.Version1)
			data = append(data, []byte{0, 0}...)                         // conn ID lens
			data = append(data, []byte{'f', 'o', 'o', 'b', 'a', 'r'}...) // token
			data = append(data, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}...)
			// this results in a token length of 0
			_, _, _, err := ParsePacket(data)
			Expect(err).To(MatchError(io.EOF))
		})

		It("errors if the token length is too large", func() {
			data := []byte{0xc0 ^ 0x1}
			data = appendVersion(data, protocol.Version1)
			data = append(data, 0x0)                   // connection ID lengths
			data = append(data, encodeVarInt(4)...)    // token length: 4 bytes (1 byte too long)
			data = append(data, encodeVarInt(0x42)...) // length, 1 byte
			data = append(data, []byte{0x12, 0x34}...) // packet number

			_, _, _, err := ParsePacket(data)
			Expect(err).To(MatchError(io.EOF))
		})

		It("errors if the 5th or 6th bit are set", func() {
			data := []byte{0xc0 | 0x2<<4 | 0x8 /* set the 5th bit */ | 0x1 /* 2 byte packet number */}
			data = appendVersion(data, protocol.Version1)
			data = append(data, []byte{0x0, 0x0}...)   // connection ID lengths
			data = append(data, encodeVarInt(2)...)    // length
			data = append(data, []byte{0x12, 0x34}...) // packet number
			hdr, _, _, err := ParsePacket(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(hdr.Type).To(Equal(protocol.PacketTypeHandshake))
			extHdr, err := hdr.ParseExtended(bytes.NewReader(data), protocol.Version1)
			Expect(err).To(MatchError(ErrInvalidReservedBits))
			Expect(extHdr).ToNot(BeNil())
			Expect(extHdr.PacketNumber).To(Equal(protocol.PacketNumber(0x1234)))
		})

		It("errors on EOF, when parsing the header", func() {
			data := []byte{0xc0 ^ 0x2<<4}
			data = appendVersion(data, protocol.Version1)
			data = append(data, 0x8)                                                       // dest conn ID len
			data = append(data, []byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0x13, 0x37}...) // dest conn ID
			data = append(data, 0x8)                                                       // src conn ID len
			data = append(data, []byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0x13, 0x37}...) // src conn ID
			for i := 1; i < len(data); i++ {
				_, _, _, err := ParsePacket(data[:i])
				Expect(err).To(Equal(io.EOF))
			}
		})

		It("errors on EOF, when parsing the extended header", func() {
			data := []byte{0xc0 | 0x2<<4 | 0x3}
			data = appendVersion(data, protocol.Version1)
			data = append(data, []byte{0x0, 0x0}...) // connection ID lengths
			data = append(data, encodeVarInt(0)...)  // length
			hdrLen := len(data)
			data = append(data, []byte{0xde, 0xad, 0xbe, 0xef}...) // packet number
			for i := hdrLen; i < len(data); i++ {
				data = data[:i]
				hdr, _, _, err := ParsePacket(data)
				Expect(err).ToNot(HaveOccurred())
				b := bytes.NewReader(data)
				_, err = hdr.ParseExtended(b, protocol.Version1)
				Expect(err).To(Equal(io.EOF))
			}
		})

		It("errors on EOF, for a Retry packet", func() {
			data := []byte{0xc0 ^ 0x3<<4}
			data = appendVersion(data, protocol.Version1)
			data = append(data, []byte{0x0, 0x0}...)                      // connection ID lengths
			data = append(data, 0xa)                                      // Orig Destination Connection ID length
			data = append(data, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}...) // source connection ID
			hdrLen := len(data)
			for i := hdrLen; i < len(data); i++ {
				data = data[:i]
				hdr, _, _, err := ParsePacket(data)
				Expect(err).ToNot(HaveOccurred())
				b := bytes.NewReader(data)
				_, err = hdr.ParseExtended(b, protocol.Version1)
				Expect(err).To(Equal(io.EOF))
			}
		})

		Context("coalesced packets", func() {
			It("cuts packets", func() {
				hdr := Header{
					Type:             protocol.PacketTypeInitial,
					DestConnectionID: protocol.ParseConnectionID([]byte{1, 2, 3, 4}),
					Length:           2 + 6,
					Version:          protocol.Version1,
				}
				b, err := (&ExtendedHeader{
					Header:          hdr,
					PacketNumber:    0x1337,
					PacketNumberLen: 2,
				}).Append(nil, protocol.Version1)
				Expect(err).ToNot(HaveOccurred())
				hdrRaw := append([]byte{}, b...)
				b = append(b, []byte("foobar")...) // payload of the first packet
				b = append(b, []byte("raboof")...) // second packet
				parsedHdr, data, rest, err := ParsePacket(b)
				Expect(err).ToNot(HaveOccurred())
				Expect(parsedHdr.Type).To(Equal(hdr.Type))
				Expect(parsedHdr.DestConnectionID).To(Equal(hdr.DestConnectionID))
				Expect(data).To(Equal(append(hdrRaw, []byte("foobar")...)))
				Expect(rest).To(Equal([]byte("raboof")))
			})

			It("errors on packets that are smaller than the length in the packet header, for too small packet number", func() {
				b, err := (&ExtendedHeader{
					Header: Header{
						Type:             protocol.PacketTypeInitial,
						DestConnectionID: protocol.ParseConnectionID([]byte{1, 2, 3, 4}),
						Length:           3,
						Version:          protocol.Version1,
					},
					PacketNumber:    0x1337,
					PacketNumberLen: 2,
				}).Append(nil, protocol.Version1)
				Expect(err).ToNot(HaveOccurred())
				_, _, _, err = ParsePacket(b)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("packet length (2 bytes) is smaller than the expected length (3 bytes)"))
			})

			It("errors on packets that are smaller than the length in the packet header, for too small payload", func() {
				b, err := (&ExtendedHeader{
					Header: Header{
						Type:             protocol.PacketTypeInitial,
						DestConnectionID: protocol.ParseConnectionID([]byte{1, 2, 3, 4}),
						Length:           1000,
						Version:          protocol.Version1,
					},
					PacketNumber:    0x1337,
					PacketNumberLen: 2,
				}).Append(nil, protocol.Version1)
				Expect(err).ToNot(HaveOccurred())
				b = append(b, make([]byte, 500-2 /* for packet number length */)...)
				_, _, _, err = ParsePacket(b)
				Expect(err).To(MatchError("packet length (500 bytes) is smaller than the expected length (1000 bytes)"))
			})
		})
	})

	It("distinguishes long and short header packets", func() {
		Expect(IsLongHeaderPacket(0x40)).To(BeFalse())
		Expect(IsLongHeaderPacket(0x80 ^ 0x40 ^ 0x12)).To(BeTrue())
	})

	It("tells its packet type for logging", func() {
		Expect((&Header{Type: protocol.PacketTypeInitial}).PacketType()).To(Equal("Initial"))
		Expect((&Header{Type: protocol.PacketTypeHandshake}).PacketType()).To(Equal("Handshake"))
	})
})

func BenchmarkIs0RTTPacket(b *testing.B) {
	random := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	packets := make([][]byte, 1024)
	for i := 0; i < len(packets); i++ {
		packets[i] = make([]byte, random.Intn(256))
		random.Read(packets[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Is0RTTPacket(packets[i%len(packets)])
	}
}

func BenchmarkParseInitial(b *testing.B) {
	b.Run("without token", func(b *testing.B) {
		benchmarkInitialPacketParsing(b, nil)
	})
	b.Run("with token", func(b *testing.B) {
		token := make([]byte, 32)
		rand.Read(token)
		benchmarkInitialPacketParsing(b, token)
	})
}

func benchmarkInitialPacketParsing(b *testing.B, token []byte) {
	hdr := Header{
		Type:             protocol.PacketTypeInitial,
		DestConnectionID: protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}),
		SrcConnectionID:  protocol.ParseConnectionID([]byte{8, 7, 6, 5, 4, 3, 2, 1}),
		Length:           1000,
		Token:            token,
		Version:          protocol.Version1,
	}
	data, err := (&ExtendedHeader{
		Header:          hdr,
		PacketNumber:    0x1337,
		PacketNumberLen: 4,
	}).Append(nil, protocol.Version1)
	if err != nil {
		b.Fatal(err)
	}
	data = append(data, make([]byte, 1000)...)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h, _, _, err := ParsePacket(data)
		if err != nil {
			b.Fatal(err)
		}
		if h.Type != hdr.Type || h.DestConnectionID != hdr.DestConnectionID || h.SrcConnectionID != hdr.SrcConnectionID ||
			!bytes.Equal(h.Token, hdr.Token) {
			b.Fatalf("headers don't match: %v vs %v", h, hdr)
		}
	}
}

func BenchmarkParseRetry(b *testing.B) {
	token := make([]byte, 64)
	rand.Read(token)
	hdr := &ExtendedHeader{
		Header: Header{
			Type:             protocol.PacketTypeRetry,
			SrcConnectionID:  protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}),
			DestConnectionID: protocol.ParseConnectionID([]byte{8, 7, 6, 5, 4, 3, 2, 1}),
			Token:            token,
			Version:          protocol.Version1,
		},
	}
	data, err := hdr.Append(nil, hdr.Version)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h, _, _, err := ParsePacket(data)
		if err != nil {
			b.Fatal(err)
		}
		if h.Type != hdr.Type || h.DestConnectionID != hdr.DestConnectionID || h.SrcConnectionID != hdr.SrcConnectionID ||
			!bytes.Equal(h.Token, hdr.Token[:len(hdr.Token)-16]) {
			b.Fatalf("headers don't match: %#v vs %#v", h, hdr)
		}
	}
}
