package eruntime

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type eraSaveFileType byte

const (
	eraSaveNormal  eraSaveFileType = 0x00
	eraSaveGlobal  eraSaveFileType = 0x01
	eraSaveVar     eraSaveFileType = 0x02
	eraSaveCharVar eraSaveFileType = 0x03
)

type eraSaveDataType byte

const (
	eraTypeInt      eraSaveDataType = 0x00
	eraTypeIntArray eraSaveDataType = 0x01
	eraTypeInt2D    eraSaveDataType = 0x02
	eraTypeInt3D    eraSaveDataType = 0x03
	eraTypeStr      eraSaveDataType = 0x10
	eraTypeStrArray eraSaveDataType = 0x11
	eraTypeStr2D    eraSaveDataType = 0x12
	eraTypeStr3D    eraSaveDataType = 0x13
	eraTypeSep      eraSaveDataType = 0xFD
	eraTypeEOC      eraSaveDataType = 0xFE
	eraTypeEOF      eraSaveDataType = 0xFF
)

const (
	eraBDHeader  uint64 = 0x0A1A0A0D41524589
	eraBDVersion uint32 = 1808
	eraBDDataCnt uint32 = 0
)

const (
	ebByte   byte = 0xCF
	ebInt16  byte = 0xD0
	ebInt32  byte = 0xD1
	ebInt64  byte = 0xD2
	ebString byte = 0xD8
	ebEoA1   byte = 0xE0
	ebEoA2   byte = 0xE1
	ebZero   byte = 0xF0
	ebZeroA1 byte = 0xF1
	ebZeroA2 byte = 0xF2
	ebEoD    byte = 0xFF
)

type eraBinaryWriter struct {
	f *os.File
	w *bytes.Buffer
}

func newEraBinaryWriter(path string) (*eraBinaryWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &eraBinaryWriter{f: f, w: &bytes.Buffer{}}, nil
}

func (bw *eraBinaryWriter) Close() error {
	if bw == nil {
		return nil
	}
	if bw.f == nil {
		return nil
	}
	if _, err := bw.f.Write(bw.w.Bytes()); err != nil {
		_ = bw.f.Close()
		bw.f = nil
		return err
	}
	err := bw.f.Close()
	bw.f = nil
	return err
}

func (bw *eraBinaryWriter) writeHeader() {
	_ = binary.Write(bw.w, binary.LittleEndian, eraBDHeader)
	_ = binary.Write(bw.w, binary.LittleEndian, eraBDVersion)
	_ = binary.Write(bw.w, binary.LittleEndian, eraBDDataCnt)
}

func (bw *eraBinaryWriter) writeFileType(t eraSaveFileType) {
	bw.w.WriteByte(byte(t))
}

func (bw *eraBinaryWriter) writeInt64(v int64) {
	_ = binary.Write(bw.w, binary.LittleEndian, v)
}

func (bw *eraBinaryWriter) writeDotNetString(s string) {
	if s == "" {
		bw.write7BitEncodedInt(0)
		return
	}
	runes := []rune(s)
	u16 := utf16.Encode(runes)
	byteLen := len(u16) * 2
	bw.write7BitEncodedInt(byteLen)
	for _, u := range u16 {
		_ = binary.Write(bw.w, binary.LittleEndian, u)
	}
}

func (bw *eraBinaryWriter) write7BitEncodedInt(v int) {
	uv := uint(v)
	for uv >= 0x80 {
		bw.w.WriteByte(byte(uv | 0x80))
		uv >>= 7
	}
	bw.w.WriteByte(byte(uv))
}

func (bw *eraBinaryWriter) writeSeparator() { bw.w.WriteByte(byte(eraTypeSep)) }
func (bw *eraBinaryWriter) writeEOC()       { bw.w.WriteByte(byte(eraTypeEOC)) }
func (bw *eraBinaryWriter) writeEOF()       { bw.w.WriteByte(byte(eraTypeEOF)) }

func (bw *eraBinaryWriter) writeCompressedInt(v int64) {
	if v >= 0 && v <= int64(ebByte) {
		bw.w.WriteByte(byte(v))
		return
	}
	if v >= math.MinInt16 && v <= math.MaxInt16 {
		bw.w.WriteByte(ebInt16)
		_ = binary.Write(bw.w, binary.LittleEndian, int16(v))
		return
	}
	if v >= math.MinInt32 && v <= math.MaxInt32 {
		bw.w.WriteByte(ebInt32)
		_ = binary.Write(bw.w, binary.LittleEndian, int32(v))
		return
	}
	bw.w.WriteByte(ebInt64)
	_ = binary.Write(bw.w, binary.LittleEndian, v)
}

func (bw *eraBinaryWriter) writeWithKeyInt(key string, v int64) {
	bw.w.WriteByte(byte(eraTypeInt))
	bw.writeDotNetString(key)
	bw.writeCompressedInt(v)
}

func (bw *eraBinaryWriter) writeWithKeyStr(key string, s string) {
	bw.w.WriteByte(byte(eraTypeStr))
	bw.writeDotNetString(key)
	bw.writeDotNetString(s)
}

func (bw *eraBinaryWriter) writeWithKeyInt1D(key string, arr []int64) {
	bw.w.WriteByte(byte(eraTypeIntArray))
	bw.writeDotNetString(key)
	_ = binary.Write(bw.w, binary.LittleEndian, int32(len(arr)))
	countZero := 0
	for _, v := range arr {
		if v == 0 {
			countZero++
			continue
		}
		if countZero > 0 {
			bw.w.WriteByte(ebZero)
			bw.writeCompressedInt(int64(countZero))
			countZero = 0
		}
		bw.writeCompressedInt(v)
	}
	bw.w.WriteByte(ebEoD)
}

func (bw *eraBinaryWriter) writeWithKeyInt2D(key string, arr []int64, d0, d1 int) {
	bw.w.WriteByte(byte(eraTypeInt2D))
	bw.writeDotNetString(key)
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d0))
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d1))
	countZero := 0
	countAllZero := 0
	for x := 0; x < d0; x++ {
		for y := 0; y < d1; y++ {
			v := arr[x*d1+y]
			if v == 0 {
				countZero++
				continue
			}
			if countAllZero > 0 {
				bw.w.WriteByte(ebZeroA1)
				bw.writeCompressedInt(int64(countAllZero))
				countAllZero = 0
			}
			if countZero > 0 {
				bw.w.WriteByte(ebZero)
				bw.writeCompressedInt(int64(countZero))
				countZero = 0
			}
			bw.writeCompressedInt(v)
		}
		if countZero == d1 {
			countAllZero++
		} else {
			bw.w.WriteByte(ebEoA1)
		}
		countZero = 0
	}
	bw.w.WriteByte(ebEoD)
}

func (bw *eraBinaryWriter) writeWithKeyInt3D(key string, arr []int64, d0, d1, d2 int) {
	bw.w.WriteByte(byte(eraTypeInt3D))
	bw.writeDotNetString(key)
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d0))
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d1))
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d2))
	countZero := 0
	countAllZero := 0
	countAllZero2D := 0
	for x := 0; x < d0; x++ {
		for y := 0; y < d1; y++ {
			for z := 0; z < d2; z++ {
				v := arr[(x*d1+y)*d2+z]
				if v == 0 {
					countZero++
					continue
				}
				if countAllZero2D > 0 {
					bw.w.WriteByte(ebZeroA2)
					bw.writeCompressedInt(int64(countAllZero2D))
					countAllZero2D = 0
				}
				if countAllZero > 0 {
					bw.w.WriteByte(ebZeroA1)
					bw.writeCompressedInt(int64(countAllZero))
					countAllZero = 0
				}
				if countZero > 0 {
					bw.w.WriteByte(ebZero)
					bw.writeCompressedInt(int64(countZero))
					countZero = 0
				}
				bw.writeCompressedInt(v)
			}
			if countZero == d2 {
				countAllZero++
			} else {
				bw.w.WriteByte(ebEoA1)
			}
			countZero = 0
		}
		if countAllZero == d1 {
			countAllZero2D++
		} else {
			bw.w.WriteByte(ebEoA2)
		}
		countAllZero = 0
	}
	bw.w.WriteByte(ebEoD)
}

func (bw *eraBinaryWriter) writeWithKeyStr1D(key string, arr []string) {
	bw.w.WriteByte(byte(eraTypeStrArray))
	bw.writeDotNetString(key)
	_ = binary.Write(bw.w, binary.LittleEndian, int32(len(arr)))
	countZero := 0
	for _, s := range arr {
		if s == "" {
			countZero++
			continue
		}
		if countZero > 0 {
			bw.w.WriteByte(ebZero)
			bw.writeCompressedInt(int64(countZero))
			countZero = 0
		}
		bw.w.WriteByte(ebString)
		bw.writeDotNetString(s)
	}
	bw.w.WriteByte(ebEoD)
}

func (bw *eraBinaryWriter) writeWithKeyStr2D(key string, arr []string, d0, d1 int) {
	bw.w.WriteByte(byte(eraTypeStr2D))
	bw.writeDotNetString(key)
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d0))
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d1))
	countZero := 0
	countAllZero := 0
	for x := 0; x < d0; x++ {
		for y := 0; y < d1; y++ {
			s := arr[x*d1+y]
			if s == "" {
				countZero++
				continue
			}
			if countAllZero > 0 {
				bw.w.WriteByte(ebZeroA1)
				bw.writeCompressedInt(int64(countAllZero))
				countAllZero = 0
			}
			if countZero > 0 {
				bw.w.WriteByte(ebZero)
				bw.writeCompressedInt(int64(countZero))
				countZero = 0
			}
			bw.w.WriteByte(ebString)
			bw.writeDotNetString(s)
		}
		if countZero == d1 {
			countAllZero++
		} else {
			bw.w.WriteByte(ebEoA1)
		}
		countZero = 0
	}
	bw.w.WriteByte(ebEoD)
}

func (bw *eraBinaryWriter) writeWithKeyStr3D(key string, arr []string, d0, d1, d2 int) {
	bw.w.WriteByte(byte(eraTypeStr3D))
	bw.writeDotNetString(key)
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d0))
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d1))
	_ = binary.Write(bw.w, binary.LittleEndian, int32(d2))
	countZero := 0
	countAllZero := 0
	countAllZero2D := 0
	for x := 0; x < d0; x++ {
		for y := 0; y < d1; y++ {
			for z := 0; z < d2; z++ {
				s := arr[(x*d1+y)*d2+z]
				if s == "" {
					countZero++
					continue
				}
				if countAllZero2D > 0 {
					bw.w.WriteByte(ebZeroA2)
					bw.writeCompressedInt(int64(countAllZero2D))
					countAllZero2D = 0
				}
				if countAllZero > 0 {
					bw.w.WriteByte(ebZeroA1)
					bw.writeCompressedInt(int64(countAllZero))
					countAllZero = 0
				}
				if countZero > 0 {
					bw.w.WriteByte(ebZero)
					bw.writeCompressedInt(int64(countZero))
					countZero = 0
				}
				bw.w.WriteByte(ebString)
				bw.writeDotNetString(s)
			}
			if countZero == d2 {
				countAllZero++
			} else {
				bw.w.WriteByte(ebEoA1)
			}
			countZero = 0
		}
		if countAllZero == d1 {
			countAllZero2D++
		} else {
			bw.w.WriteByte(ebEoA2)
		}
		countAllZero = 0
	}
	bw.w.WriteByte(ebEoD)
}

type eraBinaryReader struct {
	r       *bytes.Reader
	version uint32
}

func newEraBinaryReader(b []byte) (*eraBinaryReader, error) {
	r := bytes.NewReader(b)
	var hdr uint64
	if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		return nil, err
	}
	if hdr != eraBDHeader {
		return nil, errors.New("invalid era binary header")
	}
	var ver uint32
	if err := binary.Read(r, binary.LittleEndian, &ver); err != nil {
		return nil, err
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	for i := uint32(0); i < count; i++ {
		var dummy uint32
		if err := binary.Read(r, binary.LittleEndian, &dummy); err != nil {
			return nil, err
		}
	}
	if ver != eraBDVersion {
		return nil, fmt.Errorf("unsupported era binary version %d", ver)
	}
	return &eraBinaryReader{r: r, version: ver}, nil
}

func (br *eraBinaryReader) readByte() (byte, error) {
	return br.r.ReadByte()
}

func (br *eraBinaryReader) readFileType() (eraSaveFileType, error) {
	b, err := br.readByte()
	if err != nil {
		return 0, err
	}
	if b > byte(eraSaveCharVar) {
		return 0, fmt.Errorf("invalid save file type %d", b)
	}
	return eraSaveFileType(b), nil
}

func (br *eraBinaryReader) readInt64() (int64, error) {
	var v int64
	err := binary.Read(br.r, binary.LittleEndian, &v)
	return v, err
}

func (br *eraBinaryReader) read7BitEncodedInt() (int, error) {
	result := 0
	shift := 0
	for shift < 35 {
		b, err := br.readByte()
		if err != nil {
			return 0, err
		}
		result |= int(b&0x7F) << shift
		if (b & 0x80) == 0 {
			return result, nil
		}
		shift += 7
	}
	return 0, errors.New("invalid 7-bit encoded int")
}

func (br *eraBinaryReader) readDotNetString() (string, error) {
	n, err := br.read7BitEncodedInt()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if n < 0 || n%2 != 0 {
		return "", fmt.Errorf("invalid utf16 byte length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(br.r, buf); err != nil {
		return "", err
	}
	u16 := make([]uint16, n/2)
	for i := 0; i < len(u16); i++ {
		u16[i] = binary.LittleEndian.Uint16(buf[i*2 : i*2+2])
	}
	return string(utf16.Decode(u16)), nil
}

func (br *eraBinaryReader) readCompressedIntWithFirst(b byte) (int64, error) {
	if b <= ebByte {
		return int64(b), nil
	}
	switch b {
	case ebInt16:
		var v int16
		if err := binary.Read(br.r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int64(v), nil
	case ebInt32:
		var v int32
		if err := binary.Read(br.r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int64(v), nil
	case ebInt64:
		var v int64
		if err := binary.Read(br.r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return v, nil
	default:
		return 0, fmt.Errorf("invalid compressed int marker 0x%X", b)
	}
}

func (br *eraBinaryReader) readCompressedInt() (int64, error) {
	b, err := br.readByte()
	if err != nil {
		return 0, err
	}
	return br.readCompressedIntWithFirst(b)
}

func (br *eraBinaryReader) readVarCode() (eraSaveDataType, string, error) {
	b, err := br.readByte()
	if err != nil {
		return 0, "", err
	}
	t := eraSaveDataType(b)
	if t == eraTypeSep || t == eraTypeEOC || t == eraTypeEOF {
		return t, "", nil
	}
	key, err := br.readDotNetString()
	if err != nil {
		return 0, "", err
	}
	return t, key, nil
}

func (br *eraBinaryReader) readIntArray1D() ([]int64, []int, error) {
	var length int32
	if err := binary.Read(br.r, binary.LittleEndian, &length); err != nil {
		return nil, nil, err
	}
	if length < 0 {
		return nil, nil, fmt.Errorf("negative length")
	}
	arr := make([]int64, int(length))
	x := 0
	for {
		b, err := br.readByte()
		if err != nil {
			return nil, nil, err
		}
		if b == ebEoD {
			break
		}
		if b == ebZero {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			x += int(cnt)
			continue
		}
		v, err := br.readCompressedIntWithFirst(b)
		if err != nil {
			return nil, nil, err
		}
		if x >= 0 && x < len(arr) {
			arr[x] = v
		}
		x++
	}
	return arr, []int{int(length)}, nil
}

func (br *eraBinaryReader) readIntArray2D() ([]int64, []int, error) {
	var l0, l1 int32
	if err := binary.Read(br.r, binary.LittleEndian, &l0); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(br.r, binary.LittleEndian, &l1); err != nil {
		return nil, nil, err
	}
	if l0 < 0 || l1 < 0 {
		return nil, nil, fmt.Errorf("negative length")
	}
	d0, d1 := int(l0), int(l1)
	arr := make([]int64, d0*d1)
	x, y := 0, 0
	for {
		b, err := br.readByte()
		if err != nil {
			return nil, nil, err
		}
		if b == ebEoD {
			break
		}
		if b == ebZeroA1 {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			x += int(cnt)
			y = 0
			continue
		}
		if b == ebEoA1 {
			x++
			y = 0
			continue
		}
		if b == ebZero {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			y += int(cnt)
			continue
		}
		v, err := br.readCompressedIntWithFirst(b)
		if err != nil {
			return nil, nil, err
		}
		if x >= 0 && x < d0 && y >= 0 && y < d1 {
			arr[x*d1+y] = v
		}
		y++
	}
	return arr, []int{d0, d1}, nil
}

func (br *eraBinaryReader) readIntArray3D() ([]int64, []int, error) {
	var l0, l1, l2 int32
	if err := binary.Read(br.r, binary.LittleEndian, &l0); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(br.r, binary.LittleEndian, &l1); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(br.r, binary.LittleEndian, &l2); err != nil {
		return nil, nil, err
	}
	if l0 < 0 || l1 < 0 || l2 < 0 {
		return nil, nil, fmt.Errorf("negative length")
	}
	d0, d1, d2 := int(l0), int(l1), int(l2)
	arr := make([]int64, d0*d1*d2)
	x, y, z := 0, 0, 0
	for {
		b, err := br.readByte()
		if err != nil {
			return nil, nil, err
		}
		if b == ebEoD {
			break
		}
		if b == ebZeroA2 {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			x += int(cnt)
			y, z = 0, 0
			continue
		}
		if b == ebEoA2 {
			x++
			y, z = 0, 0
			continue
		}
		if b == ebZeroA1 {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			y += int(cnt)
			z = 0
			continue
		}
		if b == ebEoA1 {
			y++
			z = 0
			continue
		}
		if b == ebZero {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			z += int(cnt)
			continue
		}
		v, err := br.readCompressedIntWithFirst(b)
		if err != nil {
			return nil, nil, err
		}
		if x >= 0 && x < d0 && y >= 0 && y < d1 && z >= 0 && z < d2 {
			arr[(x*d1+y)*d2+z] = v
		}
		z++
	}
	return arr, []int{d0, d1, d2}, nil
}

func (br *eraBinaryReader) readStrArray1D() ([]string, []int, error) {
	var length int32
	if err := binary.Read(br.r, binary.LittleEndian, &length); err != nil {
		return nil, nil, err
	}
	if length < 0 {
		return nil, nil, fmt.Errorf("negative length")
	}
	arr := make([]string, int(length))
	x := 0
	for {
		b, err := br.readByte()
		if err != nil {
			return nil, nil, err
		}
		if b == ebEoD {
			break
		}
		if b == ebZero {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			x += int(cnt)
			continue
		}
		if b != ebString {
			return nil, nil, fmt.Errorf("invalid string array marker 0x%X", b)
		}
		s, err := br.readDotNetString()
		if err != nil {
			return nil, nil, err
		}
		if x >= 0 && x < len(arr) {
			arr[x] = s
		}
		x++
	}
	return arr, []int{int(length)}, nil
}

func (br *eraBinaryReader) readStrArray2D() ([]string, []int, error) {
	var l0, l1 int32
	if err := binary.Read(br.r, binary.LittleEndian, &l0); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(br.r, binary.LittleEndian, &l1); err != nil {
		return nil, nil, err
	}
	if l0 < 0 || l1 < 0 {
		return nil, nil, fmt.Errorf("negative length")
	}
	d0, d1 := int(l0), int(l1)
	arr := make([]string, d0*d1)
	x, y := 0, 0
	for {
		b, err := br.readByte()
		if err != nil {
			return nil, nil, err
		}
		if b == ebEoD {
			break
		}
		if b == ebZeroA1 {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			x += int(cnt)
			y = 0
			continue
		}
		if b == ebEoA1 {
			x++
			y = 0
			continue
		}
		if b == ebZero {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			y += int(cnt)
			continue
		}
		if b != ebString {
			return nil, nil, fmt.Errorf("invalid string array marker 0x%X", b)
		}
		s, err := br.readDotNetString()
		if err != nil {
			return nil, nil, err
		}
		if x >= 0 && x < d0 && y >= 0 && y < d1 {
			arr[x*d1+y] = s
		}
		y++
	}
	return arr, []int{d0, d1}, nil
}

func (br *eraBinaryReader) readStrArray3D() ([]string, []int, error) {
	var l0, l1, l2 int32
	if err := binary.Read(br.r, binary.LittleEndian, &l0); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(br.r, binary.LittleEndian, &l1); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(br.r, binary.LittleEndian, &l2); err != nil {
		return nil, nil, err
	}
	if l0 < 0 || l1 < 0 || l2 < 0 {
		return nil, nil, fmt.Errorf("negative length")
	}
	d0, d1, d2 := int(l0), int(l1), int(l2)
	arr := make([]string, d0*d1*d2)
	x, y, z := 0, 0, 0
	for {
		b, err := br.readByte()
		if err != nil {
			return nil, nil, err
		}
		if b == ebEoD {
			break
		}
		if b == ebZeroA2 {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			x += int(cnt)
			y, z = 0, 0
			continue
		}
		if b == ebEoA2 {
			x++
			y, z = 0, 0
			continue
		}
		if b == ebZeroA1 {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			y += int(cnt)
			z = 0
			continue
		}
		if b == ebEoA1 {
			y++
			z = 0
			continue
		}
		if b == ebZero {
			cnt, err := br.readCompressedInt()
			if err != nil {
				return nil, nil, err
			}
			z += int(cnt)
			continue
		}
		if b != ebString {
			return nil, nil, fmt.Errorf("invalid string array marker 0x%X", b)
		}
		s, err := br.readDotNetString()
		if err != nil {
			return nil, nil, err
		}
		if x >= 0 && x < d0 && y >= 0 && y < d1 && z >= 0 && z < d2 {
			arr[(x*d1+y)*d2+z] = s
		}
		z++
	}
	return arr, []int{d0, d1, d2}, nil
}

func parseIndexKey(key string) ([]int64, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	parts := strings.Split(key, ":")
	idx := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, false
		}
		idx = append(idx, n)
	}
	return idx, true
}

func arrayToDenseInt(arr *ArrayVar) ([]int64, []int, error) {
	if arr == nil {
		return nil, nil, fmt.Errorf("nil array")
	}
	dims := append([]int(nil), arr.Dims...)
	if len(dims) == 0 {
		dims = []int{1}
	}
	if len(dims) > 3 {
		return nil, nil, fmt.Errorf("array dimension > 3 is not supported in binary save")
	}
	total := 1
	for _, d := range dims {
		if d < 0 {
			d = 0
		}
		if d == 0 {
			total = 0
			break
		}
		total *= d
		if total > 1_000_000 {
			return nil, nil, fmt.Errorf("array too large for binary save")
		}
	}
	flat := make([]int64, total)
	for k, v := range arr.Data {
		idx, ok := parseIndexKey(k)
		if !ok || len(idx) == 0 || len(idx) > len(dims) {
			continue
		}
		off := 0
		mul := 1
		for i := len(dims) - 1; i >= 0; i-- {
			var iv int64
			if i < len(idx) {
				iv = idx[i]
			}
			if iv < 0 || iv >= int64(dims[i]) {
				off = -1
				break
			}
			off += int(iv) * mul
			mul *= dims[i]
		}
		if off >= 0 && off < len(flat) {
			flat[off] = v.Int64()
		}
	}
	return flat, dims, nil
}

func arrayToDenseStr(arr *ArrayVar) ([]string, []int, error) {
	if arr == nil {
		return nil, nil, fmt.Errorf("nil array")
	}
	dims := append([]int(nil), arr.Dims...)
	if len(dims) == 0 {
		dims = []int{1}
	}
	if len(dims) > 3 {
		return nil, nil, fmt.Errorf("array dimension > 3 is not supported in binary save")
	}
	total := 1
	for _, d := range dims {
		if d < 0 {
			d = 0
		}
		if d == 0 {
			total = 0
			break
		}
		total *= d
		if total > 1_000_000 {
			return nil, nil, fmt.Errorf("array too large for binary save")
		}
	}
	flat := make([]string, total)
	for k, v := range arr.Data {
		idx, ok := parseIndexKey(k)
		if !ok || len(idx) == 0 || len(idx) > len(dims) {
			continue
		}
		off := 0
		mul := 1
		for i := len(dims) - 1; i >= 0; i-- {
			var iv int64
			if i < len(idx) {
				iv = idx[i]
			}
			if iv < 0 || iv >= int64(dims[i]) {
				off = -1
				break
			}
			off += int(iv) * mul
			mul *= dims[i]
		}
		if off >= 0 && off < len(flat) {
			flat[off] = v.String()
		}
	}
	return flat, dims, nil
}

func denseIntToArray(flat []int64, dims []int) *ArrayVar {
	arr := newArrayVar(false, true, dims)
	if len(dims) == 0 {
		dims = []int{1}
	}
	d0 := dims[0]
	d1 := 1
	d2 := 1
	if len(dims) > 1 {
		d1 = dims[1]
	}
	if len(dims) > 2 {
		d2 = dims[2]
	}
	for i, v := range flat {
		if v == 0 {
			continue
		}
		var idx []int64
		switch len(dims) {
		case 1:
			idx = []int64{int64(i)}
		case 2:
			x := i / d1
			y := i % d1
			idx = []int64{int64(x), int64(y)}
		default:
			x := i / (d1 * d2)
			rem := i % (d1 * d2)
			y := rem / d2
			z := rem % d2
			idx = []int64{int64(x), int64(y), int64(z)}
		}
		_ = arr.Set(idx, Int(v))
		if len(dims) == 1 && i >= d0 {
			break
		}
	}
	return arr
}

func denseStrToArray(flat []string, dims []int) *ArrayVar {
	arr := newArrayVar(true, true, dims)
	if len(dims) == 0 {
		dims = []int{1}
	}
	d1 := 1
	d2 := 1
	if len(dims) > 1 {
		d1 = dims[1]
	}
	if len(dims) > 2 {
		d2 = dims[2]
	}
	for i, v := range flat {
		if v == "" {
			continue
		}
		var idx []int64
		switch len(dims) {
		case 1:
			idx = []int64{int64(i)}
		case 2:
			x := i / d1
			y := i % d1
			idx = []int64{int64(x), int64(y)}
		default:
			x := i / (d1 * d2)
			rem := i % (d1 * d2)
			y := rem / d2
			z := rem % d2
			idx = []int64{int64(x), int64(y), int64(z)}
		}
		_ = arr.Set(idx, Str(v))
	}
	return arr
}

func sortedKeysFromMap[K ~string, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}

func IsEraBinaryData(data []byte) bool {
	if len(data) < 16 {
		return false
	}
	return binary.LittleEndian.Uint64(data[:8]) == eraBDHeader
}

func (vm *VM) writeVarBinaryFile(path string, saveMes string, globals map[string]Value, arrays map[string]*ArrayVar) error {
	bw, err := newEraBinaryWriter(path)
	if err != nil {
		return err
	}
	bw.writeHeader()
	bw.writeFileType(eraSaveVar)
	bw.writeInt64(vm.saveUniqueCode)
	bw.writeInt64(vm.saveVersion)
	bw.writeDotNetString(saveMes)

	for _, key := range sortedStringKeys(globals) {
		v := globals[key]
		if v.Kind() == StringKind {
			bw.writeWithKeyStr(key, v.String())
		} else {
			bw.writeWithKeyInt(key, v.Int64())
		}
	}
	for _, key := range sortedStringKeys(arrays) {
		arr := arrays[key]
		if arr == nil {
			continue
		}
		if arr.IsString {
			flat, dims, err := arrayToDenseStr(arr)
			if err != nil {
				_ = bw.f.Close()
				return fmt.Errorf("%s: %w", key, err)
			}
			switch len(dims) {
			case 1:
				bw.writeWithKeyStr1D(key, flat)
			case 2:
				bw.writeWithKeyStr2D(key, flat, dims[0], dims[1])
			case 3:
				bw.writeWithKeyStr3D(key, flat, dims[0], dims[1], dims[2])
			default:
				_ = bw.f.Close()
				return fmt.Errorf("%s: unsupported dims %d", key, len(dims))
			}
		} else {
			flat, dims, err := arrayToDenseInt(arr)
			if err != nil {
				_ = bw.f.Close()
				return fmt.Errorf("%s: %w", key, err)
			}
			switch len(dims) {
			case 1:
				bw.writeWithKeyInt1D(key, flat)
			case 2:
				bw.writeWithKeyInt2D(key, flat, dims[0], dims[1])
			case 3:
				bw.writeWithKeyInt3D(key, flat, dims[0], dims[1], dims[2])
			default:
				_ = bw.f.Close()
				return fmt.Errorf("%s: unsupported dims %d", key, len(dims))
			}
		}
	}
	bw.writeEOF()
	if err := bw.Close(); err != nil {
		return err
	}
	return nil
}

func (vm *VM) readVarBinaryData(data []byte) (unique int64, version int64, saveMes string, globals map[string]Value, arrays map[string]*ArrayVar, err error) {
	br, err := newEraBinaryReader(data)
	if err != nil {
		return 0, 0, "", nil, nil, err
	}
	ft, err := br.readFileType()
	if err != nil {
		return 0, 0, "", nil, nil, err
	}
	if ft != eraSaveVar {
		return 0, 0, "", nil, nil, fmt.Errorf("not var save data")
	}
	unique, err = br.readInt64()
	if err != nil {
		return 0, 0, "", nil, nil, err
	}
	version, err = br.readInt64()
	if err != nil {
		return 0, 0, "", nil, nil, err
	}
	saveMes, err = br.readDotNetString()
	if err != nil {
		return 0, 0, "", nil, nil, err
	}
	globals = map[string]Value{}
	arrays = map[string]*ArrayVar{}
	for {
		typ, key, err := br.readVarCode()
		if err != nil {
			return 0, 0, "", nil, nil, err
		}
		switch typ {
		case eraTypeEOF:
			return unique, version, saveMes, globals, arrays, nil
		case eraTypeSep, eraTypeEOC:
			continue
		case eraTypeInt:
			v, err := br.readCompressedInt()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			globals[key] = Int(v)
		case eraTypeStr:
			s, err := br.readDotNetString()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			globals[key] = Str(s)
		case eraTypeIntArray:
			flat, dims, err := br.readIntArray1D()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			arrays[key] = denseIntToArray(flat, dims)
		case eraTypeInt2D:
			flat, dims, err := br.readIntArray2D()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			arrays[key] = denseIntToArray(flat, dims)
		case eraTypeInt3D:
			flat, dims, err := br.readIntArray3D()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			arrays[key] = denseIntToArray(flat, dims)
		case eraTypeStrArray:
			flat, dims, err := br.readStrArray1D()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			arrays[key] = denseStrToArray(flat, dims)
		case eraTypeStr2D:
			flat, dims, err := br.readStrArray2D()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			arrays[key] = denseStrToArray(flat, dims)
		case eraTypeStr3D:
			flat, dims, err := br.readStrArray3D()
			if err != nil {
				return 0, 0, "", nil, nil, err
			}
			arrays[key] = denseStrToArray(flat, dims)
		default:
			return 0, 0, "", nil, nil, fmt.Errorf("unsupported var data type 0x%X", byte(typ))
		}
	}
}

const charaIDKey = "__ERAGO_ID__"

func (vm *VM) writeCharaBinaryFile(path string, saveMes string, chars []RuntimeCharacter) error {
	bw, err := newEraBinaryWriter(path)
	if err != nil {
		return err
	}
	bw.writeHeader()
	bw.writeFileType(eraSaveCharVar)
	bw.writeInt64(vm.saveUniqueCode)
	bw.writeInt64(vm.saveVersion)
	bw.writeDotNetString(saveMes)
	bw.writeInt64(int64(len(chars)))
	for _, ch := range chars {
		bw.writeSeparator()
		bw.writeWithKeyInt(charaIDKey, ch.ID)
		for _, key := range sortedStringKeys(ch.Vars) {
			v := ch.Vars[key]
			if v.Kind() == StringKind {
				bw.writeWithKeyStr(key, v.String())
			} else {
				bw.writeWithKeyInt(key, v.Int64())
			}
		}
		bw.writeEOC()
	}
	bw.writeEOF()
	if err := bw.Close(); err != nil {
		return err
	}
	return nil
}

func (vm *VM) readCharaBinaryData(data []byte) (unique int64, version int64, saveMes string, chars []RuntimeCharacter, err error) {
	br, err := newEraBinaryReader(data)
	if err != nil {
		return 0, 0, "", nil, err
	}
	ft, err := br.readFileType()
	if err != nil {
		return 0, 0, "", nil, err
	}
	if ft != eraSaveCharVar {
		return 0, 0, "", nil, fmt.Errorf("not chara save data")
	}
	unique, err = br.readInt64()
	if err != nil {
		return 0, 0, "", nil, err
	}
	version, err = br.readInt64()
	if err != nil {
		return 0, 0, "", nil, err
	}
	saveMes, err = br.readDotNetString()
	if err != nil {
		return 0, 0, "", nil, err
	}
	count, err := br.readInt64()
	if err != nil {
		return 0, 0, "", nil, err
	}
	if count < 0 {
		count = 0
	}
	chars = make([]RuntimeCharacter, 0, count)
	for i := int64(0); i < count; i++ {
		id := int64(-1)
		vars := map[string]Value{}
		for {
			typ, key, err := br.readVarCode()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return unique, version, saveMes, chars, nil
				}
				return 0, 0, "", nil, err
			}
			if typ == eraTypeSep {
				continue
			}
			if typ == eraTypeEOC {
				break
			}
			if typ == eraTypeEOF {
				if id < 0 {
					id = int64(len(chars))
				}
				chars = append(chars, RuntimeCharacter{ID: id, Vars: vars})
				return unique, version, saveMes, chars, nil
			}
			switch typ {
			case eraTypeInt:
				v, err := br.readCompressedInt()
				if err != nil {
					return 0, 0, "", nil, err
				}
				if key == charaIDKey {
					id = v
				} else {
					vars[key] = Int(v)
				}
			case eraTypeStr:
				s, err := br.readDotNetString()
				if err != nil {
					return 0, 0, "", nil, err
				}
				if key == charaIDKey {
					if parsed, perr := strconv.ParseInt(strings.TrimSpace(s), 10, 64); perr == nil {
						id = parsed
					}
				} else {
					vars[key] = Str(s)
				}
			default:
				return 0, 0, "", nil, fmt.Errorf("unsupported chara data type 0x%X", byte(typ))
			}
		}
		if id < 0 {
			id = int64(len(chars))
		}
		chars = append(chars, RuntimeCharacter{ID: id, Vars: vars})
	}
	return unique, version, saveMes, chars, nil
}
